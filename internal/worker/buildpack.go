package worker

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/deployhub-io/deployhub/internal/model"
)

func prepareDockerfile(buildContext string, project model.Project) (string, string, error) {
	if project.DockerfilePath != "" {
		filename, err := safeJoin(buildContext, project.DockerfilePath)
		if err != nil {
			return "", "", err
		}
		if _, err := os.Stat(filename); err != nil {
			return "", "", fmt.Errorf("Dockerfile %q not found", project.DockerfilePath)
		}
		return filename, "Dockerfile", nil
	}
	defaultDockerfile := filepath.Join(buildContext, "Dockerfile")
	if _, err := os.Stat(defaultDockerfile); err == nil {
		return defaultDockerfile, "Dockerfile", nil
	}
	if project.Runtime == "dockerfile" {
		return "", "", fmt.Errorf("runtime requires a Dockerfile")
	}
	runtimeName := project.Runtime
	if runtimeName == "" || runtimeName == "auto" {
		runtimeName = detectRuntime(buildContext)
	}
	template, ok := buildpackTemplates[runtimeName]
	if !ok {
		return "", "", fmt.Errorf("could not detect a supported runtime")
	}
	filename := filepath.Join(buildContext, ".deployhub.Dockerfile")
	template = strings.ReplaceAll(template, "{{PORT}}", fmt.Sprintf("%d", project.Port))
	if err := os.WriteFile(filename, []byte(template), 0644); err != nil {
		return "", "", fmt.Errorf("create generated Dockerfile: %w", err)
	}
	return filename, runtimeName, nil
}

func detectRuntime(directory string) string {
	if exists(filepath.Join(directory, "package.json")) {
		return "node"
	}
	if exists(filepath.Join(directory, "requirements.txt")) || exists(filepath.Join(directory, "pyproject.toml")) {
		return "python"
	}
	if exists(filepath.Join(directory, "pom.xml")) || exists(filepath.Join(directory, "gradlew")) {
		return "java"
	}
	return ""
}

func exists(filename string) bool {
	info, err := os.Stat(filename)
	return err == nil && !info.IsDir()
}

var buildpackTemplates = map[string]string{
	"node": `FROM node:22-alpine
WORKDIR /app
COPY . .
RUN if [ -f package-lock.json ]; then npm ci --omit=dev; else npm install --omit=dev; fi
ENV NODE_ENV=production
ENV PORT={{PORT}}
EXPOSE {{PORT}}
CMD ["npm", "start"]
`,
	"python": `FROM python:3.12-slim
ENV PYTHONDONTWRITEBYTECODE=1
ENV PYTHONUNBUFFERED=1
WORKDIR /app
COPY . .
RUN if [ -f requirements.txt ]; then pip install --no-cache-dir -r requirements.txt; elif [ -f pyproject.toml ]; then pip install --no-cache-dir .; fi
ENV PORT={{PORT}}
EXPOSE {{PORT}}
CMD ["sh", "-c", "if [ -f main.py ]; then python main.py; else uvicorn app:app --host 0.0.0.0 --port ${PORT}; fi"]
`,
	"java": `FROM maven:3.9-eclipse-temurin-21-alpine AS build
WORKDIR /src
COPY . .
RUN if [ -x ./mvnw ]; then ./mvnw -DskipTests package; elif [ -x ./gradlew ]; then ./gradlew build -x test; else mvn -DskipTests package; fi && find . -type f -path "*/build/libs/*.jar" -o -path "*/target/*.jar" | grep -v sources | head -n 1 | xargs -I{} cp {} /tmp/app.jar
FROM eclipse-temurin:21-jre-alpine
WORKDIR /app
COPY --from=build /tmp/app.jar /app/app.jar
ENV PORT={{PORT}}
EXPOSE {{PORT}}
CMD ["sh", "-c", "java -jar /app/app.jar --server.port=${PORT}"]
`,
}
