package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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

type App struct {
	db *pgxpool.Pool
}

func initDB() (*pgxpool.Pool, error) {
	dbPassword := os.Getenv(DbPasswordEnvKey)
	dbUser := os.Getenv(DbUserEnvKey)
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

	// config, err := pgx.ParseConfig(
	// 	fmt.Sprintf(
	// 		"postgres://%s:%s@%s:%s/%s?sslmode=disable",
	// 		dbUser, dbPassword, dbHost, dbPort, dbName,
	// 	),
	// )
	// if err != nil {
	// 	return nil, err
	// }

	ctx, cancel := context.WithTimeout(
		context.Background(), dbConnectionTimeout,
	)
	defer cancel()

	pool, err := pgxpool.New(
		ctx, fmt.Sprintf(
			"postgres://%s:%s@%s:%s/%s?sslmode=disable",
			dbUser, dbPassword, dbHost, dbPort, dbName,
		),
	)
	// conn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		return nil, err
	}

	log.Printf("Connected to DB %s:%s\n", dbHost, dbPort)

	_, err = pool.Exec(
		context.Background(),
		"CREATE TABLE IF NOT EXISTS users (id SERIAL PRIMARY KEY, name TEXT NOT NULL);",
	)
	if err != nil {
		return nil, err
	}

	return pool, nil
}

func initApp() (*App, error) {
	db, err := initDB()
	if err != nil {
		log.Fatalf("Failed to init db: %v\n", err)
		return nil, err
	}

	return &App{db}, err
}

type User struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type GetUsersResponse struct {
	Users []User `json:"users"`
}

func (app *App) handleGetUsers(w http.ResponseWriter, r *http.Request) {
	_ = r
	w.Header().Set("Content-Type", "application/json")

	rows, err := app.db.Query(r.Context(), "SELECT * FROM users;")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	users := make([]User, 0)
	for rows.Next() {
		var id int
		var name string
		err = rows.Scan(&id, &name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		users = append(users, User{ID: id, Name: name})
	}

	response := GetUsersResponse{Users: users}
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Error encoding JSON response: %v\n", err)
	}
}

type AddUserRequest struct {
	Name string `json:"name"`
}

type UserResponse struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func (app *App) handleAddUser(w http.ResponseWriter, r *http.Request) {
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(r.Body)

	w.Header().Set("Content-Type", "application/json")

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var req AddUserRequest
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, `{"error": "Invalid request payload"}`, http.StatusBadRequest)
		log.Printf("Error decoding request body: %v\n", err)
		return
	}

	if req.Name == "" {
		http.Error(w, `{"error": "Name is required"}`, http.StatusBadRequest)
		return
	}

	var newUserID int
	err := app.db.QueryRow(
		r.Context(),
		"INSERT INTO users (name) VALUES ($1) RETURNING id",
		req.Name,
	).Scan(&newUserID)

	if err != nil {
		http.Error(w, `{"error": "Failed to add user to database"}`, http.StatusInternalServerError)
		log.Printf("Error inserting user: %v\n", err)
		return
	}

	w.WriteHeader(http.StatusCreated)
	response := UserResponse{ID: newUserID, Name: req.Name}
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Error encoding JSON response: %v\n", err)
	}
}

func (app *App) handleHealthCheck(w http.ResponseWriter, r *http.Request) {
	_ = r

	ctx, cancel := context.WithTimeout(context.Background(), dbPingTimeout)
	defer cancel()

	err := app.db.Ping(ctx)
	if err != nil {
		log.Printf("Health check ERROR: %v\n", err.Error())
		w.WriteHeader(http.StatusBadGateway)
		return
	}

	log.Printf("Health check OK")
}

func main() {
	app, err := initApp()
	if err != nil {
		log.Fatalf("Failed to init app: %v\n", err)
		return
	}

	http.HandleFunc(
		"/api/users", func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet:
				app.handleGetUsers(w, r)
			case http.MethodPost:
				app.handleAddUser(w, r)
			}
		},
	)

	http.HandleFunc(
		"/_internal/health", app.handleHealthCheck,
	)

	port := os.Getenv(AppPortEnvKey)
	if port == "" {
		port = "8080"
	}
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
