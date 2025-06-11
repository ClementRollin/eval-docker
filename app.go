package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	AppPortEnvKey       = "APP_PORT"
	DbUserEnvKey        = "DB_USER"
	DbPasswordEnvKey    = "DB_PASSWORD"
	DbHostEnvKey        = "DB_HOST"
	DbPortEnvKey        = "DB_PORT"
	DbNameEnvKey        = "DB_NAME"
	dbConnectionTimeout = 100 * time.Millisecond
	dbPingTimeout       = 10 * time.Millisecond
)

// Template for the homepage
var homeTmpl = template.Must(template.New("home").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Go Docker Exam App</title>
</head>
<body>
    <h1>Welcome to Go Docker Exam App</h1>
    <h2>Add a User</h2>
    <form action="/" method="post">
        <label for="name">Name:</label>
        <input type="text" id="name" name="name" required>
        <button type="submit">Add</button>
    </form>
    <h2>All Users</h2>
    <ul>
    {{range .Users}}
        <li>{{.ID}} - {{.Name}}</li>
    {{else}}
        <li>No users yet.</li>
    {{end}}
    </ul>
    <p><a href="/_internal/health">Health Check</a> | <a href="/api/users">JSON API</a></p>
</body>
</html>`))

// App holds the database pool
type App struct {
	db *pgxpool.Pool
}

// User represents a user record
type User struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// GetUsersResponse for JSON API
type GetUsersResponse struct {
	Users []User `json:"users"`
}

// initDB initializes the database connection and schema
func initDB() (*pgxpool.Pool, error) {
	dbUser := os.Getenv(DbUserEnvKey)
	dbPassword := os.Getenv(DbPasswordEnvKey)
	dbHost := os.Getenv(DbHostEnvKey)
	dbPort := os.Getenv(DbPortEnvKey)
	dbName := os.Getenv(DbNameEnvKey)
	if dbUser == "" {
		dbUser = "postgres"
	}
	if dbHost == "" {
		dbHost = "localhost"
	}
	if dbPort == "" {
		dbPort = "5432"
	}
	if dbName == "" {
		dbName = "postgres"
	}

	ctx, cancel := context.WithTimeout(context.Background(), dbConnectionTimeout)
	defer cancel()

	url := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", dbUser, dbPassword, dbHost, dbPort, dbName)
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}

	log.Printf("Connected to DB %s:%s", dbHost, dbPort)

	// Create table if not exists
	_, err = pool.Exec(context.Background(), `CREATE TABLE IF NOT EXISTS users (id SERIAL PRIMARY KEY, name TEXT NOT NULL);`)
	if err != nil {
		return nil, err
	}
	return pool, nil
}

// initApp sets up the App struct
func initApp() (*App, error) {
	pool, err := initDB()
	if err != nil {
		return nil, err
	}
	return &App{db: pool}, nil
}

// handleHome serves homepage and handles form submits
func (app *App) handleHome(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		rows, err := app.db.Query(r.Context(), "SELECT id, name FROM users;")
		if err != nil {
			http.Error(w, "Failed to load users", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		users := []User{}
		for rows.Next() {
			var u User
			if err := rows.Scan(&u.ID, &u.Name); err != nil {
				http.Error(w, "Error scanning user", http.StatusInternalServerError)
				return
			}
			users = append(users, u)
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		homeTmpl.Execute(w, struct{ Users []User }{Users: users})

	case http.MethodPost:
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Invalid form data", http.StatusBadRequest)
			return
		}
		name := r.FormValue("name")
		if name != "" {
			if _, err := app.db.Exec(r.Context(), "INSERT INTO users (name) VALUES ($1)", name); err != nil {
				http.Error(w, "Failed to add user", http.StatusInternalServerError)
				return
			}
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)

	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}

// handleGetUsers serves JSON API for users
func (app *App) handleGetUsers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	rows, err := app.db.Query(r.Context(), "SELECT id, name FROM users;")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	users := []User{}
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Name); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		users = append(users, u)
	}

	json.NewEncoder(w).Encode(GetUsersResponse{Users: users})
}

// handleHealthCheck for Kubernetes or orchestrators
func (app *App) handleHealthCheck(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), dbPingTimeout)
	defer cancel()

	err := app.db.Ping(ctx)
	if err != nil {
		log.Printf("Health check ERROR: %v", err)
		w.WriteHeader(http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func main() {
	app, err := initApp()
	if err != nil {
		log.Fatalf("Failed to init app: %v", err)
	}

	http.HandleFunc("/", app.handleHome)
	http.HandleFunc("/api/users", app.handleGetUsers)
	http.HandleFunc("/_internal/health", app.handleHealthCheck)

	port := os.Getenv(AppPortEnvKey)
	if port == "" {
		port = "8080"
	}
	log.Printf("Listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}