package main

import (
	"log"
	"net/http"
	"os"

	"github.com/zserge/pennybase"
)

func main() {
	server, err := pennybase.NewServer("data", "templates", "static")
	if err != nil {
		log.Fatal(err)
	}
	logger := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log.Printf("%s %s", r.Method, r.URL.String())
			next.ServeHTTP(w, r)
		})
	}
	if salt := os.Getenv("SALT"); salt != "" {
		pennybase.SessionKey = salt
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Starting server on port %s...\n", port)
	log.Fatal(http.ListenAndServe(":"+port, logger(server)))

}
