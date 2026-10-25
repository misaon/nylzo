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
	if flag.NArg() > 0 {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(*out); err != nil {
		fmt.Fprintln(os.Stderr, "nylzo-vectors:", err)
		os.Exit(1)
	}
}

func run(out string) error {
	file, err := vectors.Generate()
	if err != nil {
		return err
	}
	data, err := vectors.Marshal(file)
	if err != nil {
		return err
	}
	if out == "" {
		_, err = os.Stdout.Write(data)
		return err
	}
	return os.WriteFile(out, data, 0o600)
}
