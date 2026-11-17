package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"strings"
	"text/template"
)

func (app *application) createTemplateHandler(w http.ResponseWriter, r *http.Request) {
	// validate
	const maxBodySize = 1 << 20 // 1 MB
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	if err := r.ParseMultipartForm(maxBodySize); err != nil {
		app.badRequestResponse(w, errors.New("invalid upload"))
		return
	}
	defer r.MultipartForm.RemoveAll()

	templateType := strings.TrimSpace(r.FormValue("type"))
	if templateType == "" || strings.ContainsAny(templateType, `/\.`) {
		app.badRequestResponse(w, errors.New("invalid template type"))
		return
	}

	templateFile, _, err := r.FormFile("template")
	if err != nil {
		app.badRequestResponse(w, errors.New("template file is required"))
		return
	}
	defer templateFile.Close()

	schemaFile, _, err := r.FormFile("schema")
	if err != nil {
		app.badRequestResponse(w, errors.New("schema file is required"))
		return
	}
	defer schemaFile.Close()

	tmplData, err := io.ReadAll(templateFile)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	if _, err := template.New(templateType).Parse(string(tmplData)); err != nil {
		app.badRequestResponse(w, errors.New("invalid template file"))
		return
	}

	schema, err := io.ReadAll(schemaFile)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	var schemaCheck map[string]json.RawMessage
	if err := json.Unmarshal(schema, &schemaCheck); err != nil {
		app.badRequestResponse(w, errors.New("schema must be a JSON object"))
		return
	}

	// create directory and files
	path := fmt.Sprintf("%s/%s", app.cfg.TemplatesDir, templateType)

	if err := os.Mkdir(path, os.ModePerm); err != nil {
		if errors.Is(err, fs.ErrExist) {
			app.badRequestResponse(w, fmt.Errorf("template %s already exists", templateType))
		} else {
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	if err := os.WriteFile(fmt.Sprintf("%s/%s.tmpl", path, templateType), tmplData, os.ModePerm); err != nil {
		os.RemoveAll(path)
		app.serverErrorResponse(w, r, err)
		return
	}

	if err := os.WriteFile(path+"/schema.json", schema, os.ModePerm); err != nil {
		os.RemoveAll(path)
		app.serverErrorResponse(w, r, err)
		return
	}

	if err := app.writeJSON(w, http.StatusCreated, envelope{"template": templateType}); err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}
}
