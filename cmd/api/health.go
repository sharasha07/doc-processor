package main

import (
	"net/http"
)

func (app *application) healthHandler(w http.ResponseWriter, r *http.Request) {
	err := app.writeJSON(w, http.StatusOK, envelope{"status": "OK"})
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}
}
