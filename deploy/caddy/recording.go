package main

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/misaon/nylzo/internal/debugenv"
)

// requestLoggers carry request headers and paths in their entries: the error and access logs, the handlers and the
// standard library's HTTP server.
var requestLoggers = []string{"http.log", "http.handlers", "http.stdlib"}

var (
	errAccessLog      = errors.New("writes an access log, which records X-Nylzo-* headers")
	errNoDefaultLog   = errors.New("configures no default log, so Caddy's error log records X-Nylzo-* headers")
	errLogsRequests   = errors.New("records request headers")
	errTracing        = errors.New("the tracing handler exports request details")
	errTLSSecretsFile = errors.New("insecure_secrets_log writes TLS keys that decrypt every recorded request")
	errFastCGI        = errors.New("the FastCGI transport logs every request header at debug level")
	errRecordsHeaders = errors.New(
		"makes Caddy record request headers, which carry keys and passphrase verifiers; unset it",
	)
)

// refuseHeaderDumps runs where Caddy has applied --envfile, and quic-go reads QLOGDIR per connection.
func refuseHeaderDumps() error {
	if setting := debugenv.HeaderDump(os.Getenv); setting != "" {
		return fmt.Errorf("%s %w", setting, errRecordsHeaders)
	}
	return nil
}

// refuseRequestRecording rejects a configuration that would record X-Nylzo-* headers or message paths (F11).
func refuseRequestRecording(config map[string]any) error {
	var refused error
	walkObjects(config, func(object map[string]any) {
		transport, _ := object["transport"].(map[string]any)
		switch {
		case refused != nil:
		case object["insecure_secrets_log"] != nil:
			refused = errTLSSecretsFile
		case object["handler"] == "tracing":
			refused = errTracing
		case transport["protocol"] == "fastcgi":
			refused = errFastCGI
		default:
		}
	})
	if refused != nil {
		return refused
	}
	applications, _ := config["apps"].(map[string]any)
	web, _ := applications["http"].(map[string]any)
	servers, _ := web["servers"].(map[string]any)
	for name, value := range servers {
		server, _ := value.(map[string]any)
		if _, ok := server["logs"]; ok {
			return fmt.Errorf("server %s %w", name, errAccessLog)
		}
	}
	logging, _ := config["logging"].(map[string]any)
	logs, _ := logging["logs"].(map[string]any)
	if _, ok := logs["default"]; !ok {
		return errNoDefaultLog
	}
	for name, value := range logs {
		if err := checkLog(value); err != nil {
			return fmt.Errorf("log %s %w", name, err)
		}
	}
	return nil
}

func checkLog(value any) error {
	log, _ := value.(map[string]any)
	if writer, _ := log["writer"].(map[string]any); writer["output"] == "discard" {
		return nil
	}
	include, exclude := stringList(log["include"]), stringList(log["exclude"])
	for _, logger := range requestLoggers {
		covered := func(name string) bool { return name == logger || strings.HasPrefix(logger, name+".") }
		overlapping := func(name string) bool { return covered(name) || strings.HasPrefix(name, logger+".") }
		if len(include) == 0 && !slices.ContainsFunc(exclude, covered) || slices.ContainsFunc(include, overlapping) {
			return fmt.Errorf("%w through %s", errLogsRequests, logger)
		}
	}
	return nil
}

// markChecked tells every nylzo_frontend that this adapter checked the configuration, so the frontend can refuse one
// that skipped it.
func markChecked(config map[string]any) {
	walkObjects(config, func(object map[string]any) {
		if object["handler"] == frontendHandler {
			object[loggingCheckedField] = true
		}
	})
}

func walkObjects(value any, visit func(map[string]any)) {
	switch typed := value.(type) {
	case map[string]any:
		visit(typed)
		for _, child := range typed {
			walkObjects(child, visit)
		}
	case []any:
		for _, child := range typed {
			walkObjects(child, visit)
		}
	default:
	}
}
