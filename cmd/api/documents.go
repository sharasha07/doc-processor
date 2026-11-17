package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"slices"
	"strings"
	"text/template"
	"time"
	"uuid"

	"github.com/fabiomarini/wordingo"
	"github.com/go-pdf/fpdf"
	"github.com/rahulpoonia29/extractous-go"
	"github.com/redis/go-redis/v9"
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
	if err != nil && !errors.Is(err, io.EOF) {
		app.serverErrorResponse(w, r, err)
		return
	}

	if n == 0 {
		app.badRequestResponse(w, errors.New("document is empty"))
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
	entries, err := os.ReadDir(app.cfg.TemplatesDir)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}

	prompt := fmt.Sprintf(`Based on the document, determine its type from the following list: %s. As output return only 1 name
		of the type from the following list and nothing else, if it doesn't match any of them, just return: none`,
		strings.Join(names, " "))

	var content []*genai.Content
	if len(text) > 1000 {
		content = genai.Text(text[:1000] + prompt)
	} else {
		content = genai.Text(text + prompt)
	}

	start := time.Now()
	resp, err := app.geminiClient.Models.GenerateContent(
		r.Context(),
		app.cfg.Gemini.Model,
		content,
		nil,
	)
	geminiProcessingTimeSeconds.Add(time.Since(start).Seconds())

	if err != nil {
		geminiTaskStatus.Add("detect_document_type_fail", 1)
		app.serverErrorResponse(w, r, err)
		return
	}
	geminiTaskStatus.Add("detect_document_type_success", 1)

	if resp.UsageMetadata != nil {
		geminiInputTokens.Add(int64(resp.UsageMetadata.PromptTokenCount))
		geminiOutputTokens.Add(int64(resp.UsageMetadata.CandidatesTokenCount))
		geminiTotalTokens.Add(int64(resp.UsageMetadata.TotalTokenCount))
		geminiEstimatedCostUSD.Add(float64(resp.UsageMetadata.PromptTokenCount)/1e6*app.cfg.Gemini.InputPricePerMTokens +
			float64(resp.UsageMetadata.CandidatesTokenCount)/1e6*app.cfg.Gemini.OutputPricePerMTokens)
	}

	templateType := strings.TrimSpace(resp.Text())

	if !slices.Contains(names, templateType) {
		app.badRequestResponse(w, errors.New("unsupported document template"))
		return
	}

	// send schema to LLM to fill it and return as JSON
	path := fmt.Sprintf("%s/%s/schema.json", app.cfg.TemplatesDir, templateType)
	schema, err := os.ReadFile(path)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	prompt = `Use this schema to fill in information from the document. If something is missing from the document, fill the
			corresponding fields with zero values. Don't add anything. Return a raw JSON object only.`

	content = genai.Text(text + string(schema) + prompt)

	start = time.Now()
	resp, err = app.geminiClient.Models.GenerateContent(
		r.Context(),
		app.cfg.Gemini.Model,
		content,
		nil,
	)
	geminiProcessingTimeSeconds.Add(time.Since(start).Seconds())

	if err != nil {
		geminiTaskStatus.Add("fill_schema_fail", 1)
		app.serverErrorResponse(w, r, err)
		return
	}
	geminiTaskStatus.Add("fill_schema_success", 1)

	if resp.UsageMetadata != nil {
		geminiInputTokens.Add(int64(resp.UsageMetadata.PromptTokenCount))
		geminiOutputTokens.Add(int64(resp.UsageMetadata.CandidatesTokenCount))
		geminiTotalTokens.Add(int64(resp.UsageMetadata.TotalTokenCount))
		geminiEstimatedCostUSD.Add(float64(resp.UsageMetadata.PromptTokenCount)/1e6*app.cfg.Gemini.InputPricePerMTokens +
			float64(resp.UsageMetadata.CandidatesTokenCount)/1e6*app.cfg.Gemini.OutputPricePerMTokens)
	}

	documentFields := strings.TrimSpace(resp.Text())
	if !json.Valid([]byte(documentFields)) {
		app.serverErrorResponse(w, r, errors.New("gemini returned non-JSON output"))
		return
	}

	// create and insert document in redis
	fields := make(map[string]any)
	if err := json.Unmarshal([]byte(documentFields), &fields); err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	now := time.Now()
	document := data.Document{
		ID:        uuid.New(),
		Type:      templateType,
		Fields:    fields,
		CreatedAt: now,
		UpdatedAt: now,
	}

	payload, err := json.Marshal(document)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	if err := app.redisClient.Set(r.Context(), document.ID.String(), payload, app.cfg.Redis.TTL).Err(); err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	if err := app.writeJSON(w, http.StatusCreated, envelope{"document": document}); err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}
}

func (app *application) getReviewHandler(w http.ResponseWriter, r *http.Request) {
	id, err := app.readIDParam(r)
	if err != nil {
		app.notFoundResponse(w, r)
		return
	}

	value, err := app.redisClient.Get(r.Context(), id.String()).Result()
	if err != nil {
		switch {
		case errors.Is(err, redis.Nil):
			app.notFoundResponse(w, r)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	var document data.Document
	if err := json.Unmarshal([]byte(value), &document); err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	if err := app.writeJSON(w, http.StatusOK, envelope{"document": document}); err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}
}

func (app *application) updateReviewHandler(w http.ResponseWriter, r *http.Request) {
	id, err := app.readIDParam(r)
	if err != nil {
		app.notFoundResponse(w, r)
		return
	}

	input := make(map[string]any)

	if err := app.readJSON(w, r, &input); err != nil {
		app.badRequestResponse(w, err)
		return
	}

	value, err := app.redisClient.Get(r.Context(), id.String()).Result()
	if err != nil {
		switch {
		case errors.Is(err, redis.Nil):
			app.notFoundResponse(w, r)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	var document data.Document
	if err := json.Unmarshal([]byte(value), &document); err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	unknown := make([]string, 0, len(input))
	for key := range maps.Keys(input) {
		if _, ok := document.Fields[key]; !ok {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		app.badRequestResponse(w, fmt.Errorf("body contains unknown keys: %s", strings.Join(unknown, ", ")))
		return
	}

	maps.Copy(document.Fields, input)
	document.UpdatedAt = time.Now()

	payload, err := json.Marshal(document)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	ttl, err := app.redisClient.TTL(r.Context(), id.String()).Result()
	if err != nil || ttl <= 0 {
		ttl = app.cfg.Redis.TTL
	}

	if err := app.redisClient.Set(r.Context(), id.String(), payload, ttl).Err(); err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	if err := app.writeJSON(w, http.StatusOK, envelope{"document": document}); err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}
}

func (app *application) getDocumentHandler(w http.ResponseWriter, r *http.Request) {
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

	if err := app.writeJSON(w, http.StatusOK, envelope{"document": document}); err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}
}

func (app *application) deleteDocumentHandler(w http.ResponseWriter, r *http.Request) {
	id, err := app.readIDParam(r)
	if err != nil {
		app.notFoundResponse(w, r)
		return
	}

	if err = app.models.Documents.Delete(r.Context(), id); err != nil {
		switch {
		case errors.Is(err, data.ErrNoRecord):
			app.notFoundResponse(w, r)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (app *application) acceptReviewHandler(w http.ResponseWriter, r *http.Request) {
	id, err := app.readIDParam(r)
	if err != nil {
		app.notFoundResponse(w, r)
		return
	}

	value, err := app.redisClient.GetDel(r.Context(), id.String()).Result()
	if err != nil {
		switch {
		case errors.Is(err, redis.Nil):
			app.notFoundResponse(w, r)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	var document data.Document
	if err := json.Unmarshal([]byte(value), &document); err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	if err := app.models.Documents.Insert(r.Context(), &document); err != nil {
		if payload, err := json.Marshal(document); err == nil {
			if err := app.redisClient.Set(r.Context(), id.String(), payload, app.cfg.Redis.TTL).Err(); err != nil {
				app.logger.Error("failed to restore document in redis", "err", err)
			}
		}

		app.serverErrorResponse(w, r, err)
		return
	}

	if err := app.writeJSON(w, http.StatusCreated, envelope{"document": document}); err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}
}

func (app *application) rejectReviewHandler(w http.ResponseWriter, r *http.Request) {
	id, err := app.readIDParam(r)
	if err != nil {
		app.notFoundResponse(w, r)
		return
	}

	deleted, err := app.redisClient.Del(r.Context(), id.String()).Result()
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	if deleted == 0 {
		app.notFoundResponse(w, r)
		return
	}

	w.WriteHeader(http.StatusNoContent)
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

	path := fmt.Sprintf("%s/%s/%s.tmpl", app.cfg.TemplatesDir, document.Type, document.Type)
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

	path := fmt.Sprintf("%s/%s/%s.tmpl", app.cfg.TemplatesDir, document.Type, document.Type)
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
