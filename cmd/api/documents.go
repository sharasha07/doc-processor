package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"

	"github.com/fabiomarini/wordingo"
	"github.com/go-pdf/fpdf"
	"github.com/rahulpoonia29/extractous-go"
	"github.com/sharasha07/doc-processor/internal/data"
	"google.golang.org/genai"
)

func (app *application) uploadDocumentHandler(w http.ResponseWriter, r *http.Request) {
	// validate document
	const maxBodySize = 10 << 20 // 10 MB
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)

	const maxMemory = 10 << 20 // 10 MB
	if err := r.ParseMultipartForm(maxMemory); err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			app.badRequestResponse(w, errors.New("file size exceeds 10 MB limit"))
		} else {
			app.badRequestResponse(w, errors.New("invalid upload"))
		}
		return
	}
	defer r.MultipartForm.RemoveAll()

	file, _, err := r.FormFile("document")
	if err != nil {
		if errors.Is(err, http.ErrMissingFile) {
			app.badRequestResponse(w, errors.New("document is required"))
		} else {
			app.badRequestResponse(w, errors.New("invalid document"))
		}
		return
	}
	defer file.Close()

	buffer := make([]byte, 512)
	n, err := file.Read(buffer)
	if err != nil && err != io.EOF {
		app.serverErrorResponse(w, r, err)
		return
	}

	contentType := http.DetectContentType(buffer[:n])

	switch contentType {
	case "application/pdf", "image/png", "image/jpeg":
	default:
		app.badRequestResponse(w, errors.New("unsupported document type"))
		return
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	// extract text from document
	fileData, err := io.ReadAll(file)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	extractor := extractous.New()
	defer extractor.Close()

	text, _, err := extractor.ExtractBytesToString(fileData)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	// decide supported document template type
	entries, err := os.ReadDir("./templates")
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	names := make([]string, len(entries))
	for i, entry := range entries {
		names[i] = entry.Name()
	}

	prompt := fmt.Sprintf(`Based on the document, determine its type from the following list: %s. As output return only 1 name
		of the type from the following list and nothing else, if it doesn't match any of them, just return: none`,
		strings.Join(names, " "))

	content := genai.Text(text + prompt)
	resp, err := app.geminiClient.Models.GenerateContent(
		r.Context(),
		app.cfg.Gemini.Model,
		content,
		nil,
	)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	templateType := strings.TrimSpace(resp.Text())

	if !slices.Contains(names, templateType) {
		app.badRequestResponse(w, errors.New("unsupported document template"))
		return
	}

	// send schema to LLM to fill it and return as JSON
	path := fmt.Sprintf("./templates/%s/schema.json", templateType)
	schema, err := os.ReadFile(path)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	prompt = `Use this schema to fill in information from the document. If something is missing from the document, fill the
			corresponding fields with zero values. Don't add anything. Return a raw JSON object only.`

	content = genai.Text(text + string(schema) + prompt)
	resp, err = app.geminiClient.Models.GenerateContent(
		r.Context(),
		app.cfg.Gemini.Model,
		content,
		nil,
	)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	documentFields := strings.TrimSpace(resp.Text())
	if !json.Valid([]byte(documentFields)) {
		app.serverErrorResponse(w, r, errors.New("gemini returned non-JSON output"))
		return
	}

	// create and insert document in database
	fields := make(map[string]any)
	if err := json.Unmarshal([]byte(documentFields), &fields); err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	document := data.Document{
		Type:   templateType,
		Fields: fields,
	}

	if err := app.models.Documents.Insert(r.Context(), &document); err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	if err := app.writeJSON(w, http.StatusCreated, envelope{"document": document}); err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}
}

func (app *application) generateDOCXHandler(w http.ResponseWriter, r *http.Request) {
	id, err := app.readIDParam(r)
	if err != nil {
		app.notFoundResponse(w, r)
		return
	}

	document, err := app.models.Documents.Get(r.Context(), id)
	if err != nil {
		switch {
		case errors.Is(err, data.ErrNoRecord):
			app.notFoundResponse(w, r)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	path := fmt.Sprintf("./templates/%s/%s.tmpl", document.Type, document.Type)
	tmpl, err := template.ParseFiles(path)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, document.Fields); err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	doc, err := wordingo.Create()
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}
	doc.AddParagraph(buf.String())

	fileName := document.Type + ".docx"
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, fileName))

	if _, err := doc.WriteTo(w); err != nil {
		app.logger.Error(err.Error())
		return
	}
}

func (app *application) generatePDFHandler(w http.ResponseWriter, r *http.Request) {
	id, err := app.readIDParam(r)
	if err != nil {
		app.notFoundResponse(w, r)
		return
	}

	document, err := app.models.Documents.Get(r.Context(), id)
	if err != nil {
		switch {
		case errors.Is(err, data.ErrNoRecord):
			app.notFoundResponse(w, r)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	path := fmt.Sprintf("./templates/%s/%s.tmpl", document.Type, document.Type)
	tmpl, err := template.ParseFiles(path)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, document.Fields); err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.AddPage()
	pdf.SetFont("Arial", "", 12)
	pdf.MultiCell(0, 7, buf.String(), "", "L", false)

	if pdf.Err() {
		app.serverErrorResponse(w, r, pdf.Error())
		return
	}

	fileName := document.Type + ".pdf"
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, fileName))

	if err := pdf.Output(w); err != nil {
		app.logger.Error(err.Error())
		return
	}
}
