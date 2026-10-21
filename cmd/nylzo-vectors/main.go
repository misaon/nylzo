package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/misaon/nylzo/internal/core/vectors"
)

func main() {
	out := flag.String("out", "", "write the vectors to this file instead of stdout")
	flag.Parse()
	f, err := vectors.Generate()
	if err != nil {
		fmt.Fprintln(os.Stderr, "nylzo-vectors:", err)
		os.Exit(1)
	}
	data, err := vectors.Marshal(f)
	if err != nil {
		fmt.Fprintln(os.Stderr, "nylzo-vectors:", err)
		os.Exit(1)
	}
	if *out == "" {
		os.Stdout.Write(data)
		return
	}
	if err := os.WriteFile(*out, data, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "nylzo-vectors:", err)
		os.Exit(1)
	}
}
