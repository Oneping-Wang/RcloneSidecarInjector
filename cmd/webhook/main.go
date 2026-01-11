package main

import (
	"fmt"
	"net/http"
	"os"

	"github.com/Oneping-Wang/RcloneSidecarInjector/pkg/mutation"
)

func main() {
	http.HandleFunc("/", handleRoot)

	// 使用我们封装好的 mutation 包来处理请求
	http.HandleFunc("/mutate", mutation.HandleMutate)

	port := "8443"
	fmt.Printf("Starting Webhook Server on port %s...\n", port)

	err := http.ListenAndServe(":"+port, nil)
	if err != nil {
		fmt.Printf("Failed to start server: %v\n", err)
		os.Exit(1)
	}
}

func handleRoot(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Sidecar Injector is Alive!")
}
