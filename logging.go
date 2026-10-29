package wepi

// LogAllResponses logs "<path> response status code: N" for every response handled by Run.
// Set it to false (once, at startup) to log only responses with status >= 400.
var LogAllResponses = true
