package httpx

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
	"uuid"
)

func TestWriteJSON(t *testing.T) {
	rec := httptest.NewRecorder()

	payload := struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}{Name: "splash", Count: 3}

	if err := WriteJSON(rec, http.StatusCreated, payload); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != ContentTypeJSON {
		t.Errorf("Content-Type = %q, want %q", got, ContentTypeJSON)
	}

	body := rec.Body.String()
	if !strings.HasSuffix(body, "\n") {
		t.Error("the body should end with a newline so curl output is readable")
	}
	if !strings.Contains(body, `"name":"splash"`) || !strings.Contains(body, `"count":3`) {
		t.Errorf("body = %s", body)
	}
	// The header must describe what was actually written, newline included, or
	// the client waits for bytes that never arrive.
	if got := rec.Header().Get("Content-Length"); got != strconv.Itoa(rec.Body.Len()) {
		t.Errorf("Content-Length = %s, body is %d bytes", got, rec.Body.Len())
	}
}

// TestNilSliceEncodesAsEmptyArray pins the encoding/json/v2 behaviour the
// frontend relies on: `items: []` rather than `items: null`, so TypeScript can
// map over the array without a null guard.
func TestNilSliceEncodesAsEmptyArray(t *testing.T) {
	rec := httptest.NewRecorder()

	type payload struct {
		Items []string `json:"items"`
	}
	if err := WriteJSON(rec, http.StatusOK, payload{Items: nil}); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	if !strings.Contains(rec.Body.String(), `"items":[]`) {
		t.Errorf("body = %s, want a nil slice encoded as []", rec.Body.String())
	}
}

// TestUUIDEncodesAsAString checks that the stdlib uuid type, new in Go 1.27,
// serialises as text rather than as a base64 blob, because it implements
// encoding.TextAppender.
func TestUUIDEncodesAsAString(t *testing.T) {
	rec := httptest.NewRecorder()
	id := uuid.NewV7()

	payload := struct {
		ID uuid.UUID `json:"id"`
	}{ID: id}
	if err := WriteJSON(rec, http.StatusOK, payload); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	if want := `"id":"` + id.String() + `"`; !strings.Contains(rec.Body.String(), want) {
		t.Errorf("body = %s, want it to contain %s", rec.Body.String(), want)
	}
}

func TestTimeEncodesAsRFC3339(t *testing.T) {
	rec := httptest.NewRecorder()
	at, err := time.Parse(time.RFC3339Nano, "2026-11-21T12:00:00.500Z")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	payload := struct {
		At time.Time `json:"at"`
	}{At: at}
	if err := WriteJSON(rec, http.StatusOK, payload); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	// RFC 3339 with nanoseconds, trailing zeros trimmed — what a JavaScript
	// Date parses without help.
	if !strings.Contains(rec.Body.String(), `"at":"2026-11-21T12:00:00.5Z"`) {
		t.Errorf("body = %s", rec.Body.String())
	}
}

func TestWriteJSONReportsEncodingFailures(t *testing.T) {
	rec := httptest.NewRecorder()

	// A channel has no JSON representation, so Marshal fails before anything is
	// written — which is exactly why the value is buffered first.
	if err := WriteJSON(rec, http.StatusOK, make(chan int)); err == nil {
		t.Fatal("WriteJSON accepted an unencodable value")
	}
	if rec.Body.Len() != 0 {
		t.Errorf("a failed encode wrote %d bytes; the client must not see a partial body", rec.Body.Len())
	}
}

func TestWrite(t *testing.T) {
	rec := httptest.NewRecorder()

	Write(rec, http.StatusOK, ContentTypeHTML, []byte("<h1>hi</h1>"))

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != ContentTypeHTML {
		t.Errorf("Content-Type = %q", got)
	}
	if rec.Body.String() != "<h1>hi</h1>" {
		t.Errorf("body = %q", rec.Body.String())
	}
}

type decodeTarget struct {
	Title string `json:"title"`
	Count int    `json:"count"`
}

func postRequest(t *testing.T, body string) (*httptest.ResponseRecorder, *http.Request) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/items", strings.NewReader(body))
	req.Header.Set("Content-Type", ContentTypeJSON)
	return httptest.NewRecorder(), req
}

func TestDecodeJSON(t *testing.T) {
	rec, req := postRequest(t, `{"title":"hello","count":2}`)

	got, err := DecodeJSON[decodeTarget](rec, req, 1<<20)
	if err != nil {
		t.Fatalf("DecodeJSON: %v", err)
	}
	if got.Title != "hello" || got.Count != 2 {
		t.Errorf("decoded %+v", got)
	}
}

func TestDecodeJSONRejectsMalformedBody(t *testing.T) {
	rec, req := postRequest(t, `{"title":`)

	if _, err := DecodeJSON[decodeTarget](rec, req, 1<<20); !IsMalformedJSON(err) {
		t.Errorf("error = %v, want a JSON syntax error", err)
	}
}

func TestDecodeJSONRejectsWrongTypes(t *testing.T) {
	rec, req := postRequest(t, `{"title":"ok","count":"not a number"}`)

	_, err := DecodeJSON[decodeTarget](rec, req, 1<<20)
	if !IsMalformedJSON(err) {
		t.Errorf("error = %v, want a semantic error for the wrong type", err)
	}
	if IsPayloadTooLarge(err) {
		t.Error("a type error was reported as an oversized body")
	}
}

// TestDecodeJSONRejectsDuplicateMembers pins an encoding/json/v2 default that
// encoding/json did not have: a repeated member is an error rather than a
// silent last-one-wins. That closes a class of request-smuggling confusion.
func TestDecodeJSONRejectsDuplicateMembers(t *testing.T) {
	rec, req := postRequest(t, `{"title":"first","title":"second"}`)

	if _, err := DecodeJSON[decodeTarget](rec, req, 1<<20); !IsMalformedJSON(err) {
		t.Errorf("error = %v, want duplicate members to be rejected", err)
	}
}

// TestDecodeJSONRejectsInvalidUTF8 pins the other strict v2 default.
func TestDecodeJSONRejectsInvalidUTF8(t *testing.T) {
	rec := httptest.NewRecorder()
	// A raw 0xFF byte inside a JSON string is not valid UTF-8.
	req := httptest.NewRequest(http.MethodPost, "/items", strings.NewReader("{\"title\":\"bad\xff byte\"}"))
	req.Header.Set("Content-Type", ContentTypeJSON)

	if _, err := DecodeJSON[decodeTarget](rec, req, 1<<20); !IsMalformedJSON(err) {
		t.Errorf("error = %v, want invalid UTF-8 to be rejected", err)
	}
}

// TestDecodeJSONRequiresJSONContentType pins the CSRF hardening: a body that
// is not declared application/json (e.g. a cross-site form POST) is rejected
// before it is parsed.
func TestDecodeJSONRequiresJSONContentType(t *testing.T) {
	for _, ct := range []string{"", "text/plain", "application/x-www-form-urlencoded", "multipart/form-data"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/items", strings.NewReader(`{"title":"x"}`))
		if ct != "" {
			req.Header.Set("Content-Type", ct)
		}
		if _, err := DecodeJSON[decodeTarget](rec, req, 1<<20); !IsUnsupportedMediaType(err) {
			t.Errorf("Content-Type %q: error = %v, want unsupported media type", ct, err)
		}
	}
	// The charset parameter is fine.
	rec, req := postRequest(t, `{"title":"x"}`)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	if _, err := DecodeJSON[decodeTarget](rec, req, 1<<20); err != nil {
		t.Errorf("application/json; charset=utf-8 rejected: %v", err)
	}
}

func TestDecodeJSONEnforcesTheBodyLimit(t *testing.T) {
	rec, req := postRequest(t, `{"title":"`+strings.Repeat("x", 200)+`"}`)

	_, err := DecodeJSON[decodeTarget](rec, req, 32)
	if !IsPayloadTooLarge(err) {
		t.Errorf("error = %v, want a MaxBytesError", err)
	}
	if IsMalformedJSON(err) {
		t.Error("an oversized body was reported as malformed JSON; the client should get 413")
	}
}

func TestDecodeJSONWithoutALimitAcceptsTheWholeBody(t *testing.T) {
	rec, req := postRequest(t, `{"title":"`+strings.Repeat("x", 500)+`"}`)

	got, err := DecodeJSON[decodeTarget](rec, req, 0)
	if err != nil {
		t.Fatalf("DecodeJSON: %v", err)
	}
	if len(got.Title) != 500 {
		t.Errorf("decoded %d characters, want 500", len(got.Title))
	}
}
