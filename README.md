# Go Docker Exam App

Containerisation d'une petite appli Go.

## Usage

```bash
docker build -t exam-app .
docker run -p 8080:8080 --rm exam-app
