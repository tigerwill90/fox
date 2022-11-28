package fox

import (
	"fmt"
	"net/http"
)

func ExampleRouter_NewIterator() {
	r := New()
	it := r.NewIterator()

	// Iterate over all routes
	for it.Rewind(); it.Valid(); it.Next() {
		fmt.Println(it.Method(), it.Path())
	}

	// Iterate over all routes for the GET method
	for it.SeekMethod(http.MethodGet); it.Valid(); it.Next() {
		fmt.Println(it.Method(), it.Path())
	}

	// Iterate over all routes starting with /users
	for it.SeekPrefix("/users"); it.Valid(); it.Next() {
		fmt.Println(it.Method(), it.Path())
	}

	// Iterate over all route starting with /users for the GET method
	for it.SeekMethodPrefix(http.MethodGet, "/user"); it.Valid(); it.Next() {
		fmt.Println(it.Method(), it.Path())
	}
}
