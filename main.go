package main

import (
	"database/sql"
	"log"
	"net/http"

	_ "modernc.org/sqlite"
)

func main() {
	db, err := sql.Open("sqlite", "lab.db")
	if err != nil {
		log.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()

	if err := migrate(db); err != nil {
		log.Fatal(err)
	}

	a := newApp(db)
	log.Println("listening on http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", a.routes()))
}
