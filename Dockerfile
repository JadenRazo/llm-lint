FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" \
    -o /out/llm-lint ./cmd/llm-lint

FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/llm-lint /usr/local/bin/llm-lint
USER nonroot:nonroot
WORKDIR /workspace
ENTRYPOINT ["/usr/local/bin/llm-lint"]
CMD ["scan"]
