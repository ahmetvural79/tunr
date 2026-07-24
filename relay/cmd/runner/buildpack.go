package main

// buildpack.go — generate a SLIM Dockerfile for common stacks so app images are
// small (~120-250MB) instead of Nixpacks' ~900MB nix-store images. Node runs on
// distroless when the start is a single JS file (no shell needed), otherwise on
// alpine (keeps a shell for arbitrary start commands). Python on slim. Anything
// we don't recognise falls back to Nixpacks.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var simpleNodeStart = regexp.MustCompile(`^node\s+[\w./-]+\.(c?m?js)$`)

// generateSlimDockerfile writes a Dockerfile into dir for a recognised stack and
// returns a short label (e.g. "node · distroless"), or "" if none matched.
func generateSlimDockerfile(dir string) string {
	if fileExists(filepath.Join(dir, "package.json")) {
		start := detectNodeStart(dir)
		var df, label string
		if simpleNodeStart.MatchString(start) {
			entry := strings.TrimSpace(strings.TrimPrefix(start, "node"))
			df, label = nodeDistroless(entry), "node · distroless"
		} else {
			df, label = nodeAlpine(start), "node · alpine"
		}
		if writeDockerfile(dir, df) {
			return label
		}
	}
	if fileExists(filepath.Join(dir, "requirements.txt")) || fileExists(filepath.Join(dir, "pyproject.toml")) {
		if writeDockerfile(dir, pythonSlim(dir, detectPythonStart(dir))) {
			return "python · slim"
		}
	}
	return ""
}

// ---------- Node ----------

func detectNodeStart(dir string) string {
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
		Main    string            `json:"main"`
	}
	if b, err := os.ReadFile(filepath.Join(dir, "package.json")); err == nil {
		_ = json.Unmarshal(b, &pkg)
	}
	if s := strings.TrimSpace(pkg.Scripts["start"]); s != "" {
		return s
	}
	for _, f := range []string{"server.js", "index.js", "app.js", "main.js"} {
		if fileExists(filepath.Join(dir, f)) {
			return "node " + f
		}
	}
	if pkg.Main != "" {
		return "node " + pkg.Main
	}
	return "node server.js"
}

func nodeDistroless(entry string) string {
	return `FROM node:20-slim AS build
WORKDIR /app
COPY package*.json ./
RUN npm ci --omit=dev 2>/dev/null || npm install --omit=dev
COPY . .
RUN npm run build --if-present
FROM gcr.io/distroless/nodejs20-debian12
WORKDIR /app
COPY --from=build /app /app
ENV PORT=8080 HOST=0.0.0.0 NODE_ENV=production
EXPOSE 8080
CMD ["` + entry + `"]
`
}

func nodeAlpine(start string) string {
	return `FROM node:20-alpine AS build
WORKDIR /app
COPY package*.json ./
RUN npm ci 2>/dev/null || npm install
COPY . .
RUN npm run build --if-present
FROM node:20-alpine
WORKDIR /app
COPY --from=build /app /app
ENV PORT=8080 HOST=0.0.0.0 NODE_ENV=production
EXPOSE 8080
CMD ["sh","-c","` + shellEscape(start) + `"]
`
}

// ---------- Python ----------

func detectPythonStart(dir string) string {
	// Procfile "web:" wins.
	if b, err := os.ReadFile(filepath.Join(dir, "Procfile")); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "web:") {
				return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "web:"))
			}
		}
	}
	// Common FastAPI/Flask entrypoints.
	for _, f := range []string{"main.py", "app.py", "server.py"} {
		if fileExists(filepath.Join(dir, f)) {
			mod := strings.TrimSuffix(f, ".py")
			return "uvicorn " + mod + ":app --host 0.0.0.0 --port $PORT"
		}
	}
	return "python main.py"
}

func pythonSlim(dir, start string) string {
	install := "pip install --no-cache-dir -r requirements.txt"
	if !fileExists(filepath.Join(dir, "requirements.txt")) {
		install = "pip install --no-cache-dir ."
	}
	return `FROM python:3.12-slim
WORKDIR /app
COPY . .
RUN ` + install + ` && pip install --no-cache-dir uvicorn 2>/dev/null || true
ENV PORT=8080
EXPOSE 8080
CMD ["sh","-c","` + shellEscape(start) + `"]
`
}

// ---------- helpers ----------

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func writeDockerfile(dir, content string) bool {
	return os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(content), 0o644) == nil
}

func shellEscape(s string) string {
	return strings.ReplaceAll(s, `"`, `\"`)
}
