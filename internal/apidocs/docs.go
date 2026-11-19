// Package apidocs OpenAPI şemasını ve Swagger arayüzünü sunar. Şema derleme sırasında binary'ye gömülür.
package apidocs

import (
	_ "embed"
	"net/http"
)

//go:embed openapi.yaml
var spec []byte

// Swagger arayüzü CDN'den yüklenir; şema ise bu servisten ("/openapi.yaml") okunur.
const uiPage = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>Notification System API</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5.17.14/swagger-ui.css">
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5.17.14/swagger-ui-bundle.js"></script>
  <script>
    window.ui = SwaggerUIBundle({ url: "/openapi.yaml", dom_id: "#swagger-ui", deepLinking: true });
  </script>
</body>
</html>
`

// Spec ham OpenAPI şemasını döner.
func Spec(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	_, _ = w.Write(spec)
}

// UI Swagger arayüzü sayfasını döner.
func UI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(uiPage))
}
