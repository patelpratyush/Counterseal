# syntax=docker/dockerfile:1
FROM golang:1.26.5-alpine AS go-build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -o /out/handoffguard ./cmd/cli

FROM alpine:3.23 AS api
RUN apk add --no-cache ca-certificates && addgroup -g 10001 app && adduser -D -u 10001 -G app app \
    && mkdir /data && chown app:app /data
COPY --from=go-build /out/handoffguard /usr/local/bin/handoffguard
COPY --chmod=755 deploy/api-entrypoint.sh /usr/local/bin/api-entrypoint
USER 10001:10001
ENTRYPOINT ["api-entrypoint"]
CMD ["server", "--key", "/data/server.priv", "--addr", "127.0.0.1:8080"]

FROM maven:3.9.11-eclipse-temurin-21 AS java-build
WORKDIR /src
COPY integrations/java-workflow/pom.xml ./pom.xml
RUN mvn -B -q dependency:go-offline
COPY integrations/java-workflow/src ./src
RUN mvn -B -q package

FROM eclipse-temurin:21-jre-jammy AS demo
WORKDIR /app
COPY --from=go-build /out/handoffguard /usr/local/bin/handoffguard
COPY --from=java-build /src/target/handoffguard-workflow.jar /app/workflow.jar
ENV HANDOFFGUARD_BINARY=/usr/local/bin/handoffguard
USER 10001:10001
ENTRYPOINT ["java", "-jar", "/app/workflow.jar"]
