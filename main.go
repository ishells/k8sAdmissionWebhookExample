package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	webhook "application-standards-validating-merge-security/pkg/webhook"
)

const (
	tlsKeyName  = "tls.key"
	tlsCertName = "tls.crt"
)

func HealthCheck(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, "Health Check is OK...")
}
func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/validate", webhook.Validate)
	mux.HandleFunc("/mutate", webhook.Mutate)
	mux.HandleFunc("/health", HealthCheck)
	if certDir := os.Getenv("CERT_DIR"); certDir != "" {
		certFile := filepath.Join(certDir, tlsCertName)
		keyFile := filepath.Join(certDir, tlsKeyName)
		log.Println("serving https on 0.0.0.0:8000")
		log.Fatal(http.ListenAndServeTLS(":8000", certFile, keyFile, mux))
	} else {
		log.Println("serving http on 0.0.0.0:8000")
		log.Fatal(http.ListenAndServe(":8000", mux))
	}
}
