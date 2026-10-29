package wepi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"
)

func (w *WepiController) runUnwrapped(pathHead string, req *http.Request, wr http.ResponseWriter) (bool, error) {
	path := strings.TrimPrefix(req.URL.Path, pathHead)

	// Remove trailing slash to normalize paths (e.g. /users/ -> /users)
	if path != "/" {
		path = strings.TrimSuffix(path, "/")
	}

	// Treat PUT as POST
	if req.Method == http.MethodPut {
		req.Method = http.MethodPost
	}

	// Handle CORS preflight
	if w.optionsInterceptor(path, wr, req) {
		return true, nil
	}

	// Match path and method to a registered route
	path, route, pathParams := w.loadRouteFromRequest(path, req.Method)

	if path == "" {
		return false, nil // No matching route
	}

	if req.Method != route.method {
		log.Println("route " + route.route + " not same method " + req.Method)
		return false, errors.New("route " + route.route + " not same method " + req.Method)
	}

	// Extract handler func and its first param type (struct or ParamsManager)
	handlerFunc, stType, err := validateAndExtractRouteFunc(route)
	if err != nil {
		wr.WriteHeader(http.StatusInternalServerError)
		return true, fmt.Errorf("error on route: "+route.route+", on path "+path+":", err)
	}

	// Parse request body based on Content-Type
	values, structValue, err := readRequestValues(req, stType)
	if err != nil {
		wr.WriteHeader(http.StatusBadRequest)
		if w.ShowErrors() {
			wr.Write([]byte(err.Error()))
		}
		return true, err
	}

	hasStructBody := structValue.IsValid()

	var args []reflect.Value
	var stValue reflect.Value

	if values == nil {
		values = make(map[string]any)
	}

	params := GetParamsManager(values)

	// Merge URL path params (e.g. {id}) into the params manager
	if pathParams != nil {
		for k, v := range pathParams {
			params.data[k] = v
		}
	}

	// Build argument list for the handler function
	if stType == reflect.TypeOf((*ParamsManager)(nil)).Elem() {
		if hasStructBody {
			log.Println(errors.New("this request doesnt contain a params manager"))
			wr.WriteHeader(http.StatusInternalServerError)
			return true, errors.New("this request doesnt contain a params manager")
		}

		stValue = reflect.ValueOf(&params)
		args = []reflect.Value{
			stValue.Elem(),
			reflect.ValueOf(req),
		}
	} else {
		// Struct route: validate with go-playground/validator tags
		stValue = structValue

		validatorSingle := validatorSingleton
		validateValue := stValue.Elem()

		if validateValue.Kind() == reflect.Pointer {
			validateValue = validateValue.Elem()
		}

		if validateValue.Kind() == reflect.Struct {
			err = validatorSingle.Struct(validateValue.Interface())
			if err != nil {
				log.Println("Validator Error ", err)
				wr.WriteHeader(http.StatusUnprocessableEntity)
				msg := fmt.Sprint("Error parsing data: ", err)
				json, _ := JsonifyPretty(map[string]any{"error": msg}, "", " ")
				if ve, ok := err.(validator.ValidationErrors); ok {
					list := make([]string, 0)
					for _, fe := range ve {
						list = append(list, getValidationError(fe, validateValue.Interface()))
					}
					json, _ = JsonifyPretty(map[string]any{"error": "validation errors", "list": list}, "", " ")
				}

				wr.Write([]byte(json))
				return true, fmt.Errorf("validator Error: %v", msg)
			}
		}

		args = []reflect.Value{
			stValue.Elem(),
			reflect.ValueOf(&params).Elem(),
			reflect.ValueOf(req),
		}
	}

	// Set CORS headers
	if len(w.cors) > 0 {
		if w.isOriginAllowed(req.Header.Get("Origin")) {
			wr.Header().Set("Access-Control-Allow-Origin", req.Header.Get("Origin"))
		}
	}

	// Run middlewares; short-circuit if one returns a CustomResponse
	for _, middleware := range route.Middlewares {
		if middleware != nil {
			c, err := middleware(stValue.Elem().Interface(), params, req)
			if err != nil {
				wr.WriteHeader(http.StatusInternalServerError)
				return true, err
			}

			if c != nil {
				if c.headers != nil {
					copyHeader(wr.Header(), c.headers)
				}
				if c.status != 0 {
					wr.WriteHeader(c.status)
				}
				if len(c.body) > 0 {
					wr.Write(c.body)
				}
				return true, nil
			}
		}
	}

	// Call handler: returns (result, *CustomResponse, error)
	results := handlerFunc.Call(args)

	// Check error (third return value)
	if len(results) > 2 && !results[2].IsNil() {
		err := results[2].Interface().(error)
		log.Println("Handler returned error:", err)
		wr.WriteHeader(http.StatusInternalServerError)
		wr.Write([]byte(err.Error()))
		if w.ShowErrors() {
			wr.Write([]byte(err.Error()))
		}
		return true, fmt.Errorf("handler returned error: %v", err)
	}

	// Extract optional CustomResponse (second return value)
	var custom *CustomResponse
	if len(results) > 1 && !results[1].IsNil() {
		custom = results[1].Interface().(*CustomResponse)
	} else {
		custom = nil
	}

	// Determine response type: string (text/html), io.Reader, or struct/map (JSON)
	resultInterface := results[0].Interface()
	resultValue := reflect.ValueOf(resultInterface)

	if resultValue.Kind() == reflect.Ptr {
		resultValue = resultValue.Elem()
	}

	var js []byte

	if !resultValue.IsValid() {
		if custom == nil {
			log.Println("Validator Error: ", errors.New("no data found on route return"))
			wr.WriteHeader(http.StatusInternalServerError)
			return true, errors.New("no data found on route return")
		}
	} else if resultValue.Kind() == reflect.String {
		wr.Header().Add("Content-Type", "text/html")
		js = []byte(resultValue.String())
	} else if _, ok := resultInterface.(io.Reader); ok {
		// io.Reader handled below
	} else {
		js, err = json.Marshal(resultValue.Interface())
		if err != nil {
			log.Println("Error writing data: ", err)
			wr.Write([]byte(fmt.Sprint("error writing data: ", err)))
			wr.WriteHeader(http.StatusBadRequest)
			return true, fmt.Errorf("error writing data: %v", err)
		}
		wr.Header().Add("Content-Type", "application/json")
	}

	// Apply CustomResponse overrides if provided
	body := []byte(js)
	status := 200

	if custom != nil {
		if custom.headers != nil {
			wr.Header().Del("Content-Type")
			copyHeader(wr.Header(), custom.headers)
		}
		if custom.status != 0 {
			wr.WriteHeader(custom.status)
			status = custom.status
		}
		if len(custom.body) > 0 {
			body = custom.body
			_, err = wr.Write(body)
			return true, err
		}
	}

	// Stream io.Reader directly to client
	if r, ok := resultInterface.(io.Reader); ok {
		if rc, ok := resultInterface.(io.Closer); ok {
			defer rc.Close()
		}

		if custom.headers == nil {
			wr.Header().Set("Content-Disposition", `attachment; filename="file"`)
		}

		io.Copy(wr, r)
		return true, nil
	}

	if LogAllResponses || status >= 400 {
		log.Printf(req.URL.Path+" response status code: %v", status)
	}

	wr.Write(body)

	return true, nil
}

// Run processes incoming HTTP requests through the wepi routing system.
// Returns (true, nil) if the route was handled, (false, nil) if no route matched.
// Returned errors are enriched with the request method and path.
func (w *WepiController) Run(pathHead string, req *http.Request, wr http.ResponseWriter) (bool, error) {
	handled, err := w.runUnwrapped(pathHead, req, wr)
	if err != nil {
		return handled, fmt.Errorf("%s %s: %w", req.Method, req.URL.Path, err)
	}
	return handled, nil
}

func copyHeader(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}
