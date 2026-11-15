package main

import (
	"os"
)

func Example_lint() {
	if err := os.Chdir("testdata"); err != nil {
		panic(err)
	}
	defer func() {
		if err := os.Chdir(".."); err != nil {
			panic(err)
		}
	}()
	run([]string{"lint", "openapi.json"}, os.Stdout, os.Stdout)
	// Output:
	// openapi.json: GET /notes: error-media-type: the 400 response must be application/problem+json
	// openapi.json: GET /notes: list-cursor: collections must accept an opaque cursor
	// openapi.json: GET /notes: list-envelope: collections must return {"items": [...], "next_cursor": "..."}
	// openapi.json: GET /notes: list-limit: collections must accept limit with maximum 200
	// openapi.json: GET /notes: param-legacy: query parameter "page" must be deprecated in favour of "cursor"
	// openapi.json: GET /notes: param-name: query parameter "updatedSince" must be snake_case
	// openapi.json: POST /notes: body-closed: request objects must set additionalProperties to false so unknown fields are rejected
	// openapi.json: POST /notes: body-time-format: body field "remind_at" must declare format date-time
	// 1 document, 8 violations, 0 accepted by exceptions, 0 stale exceptions
}
