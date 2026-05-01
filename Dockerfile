FROM gcr.io/distroless/static:nonroot
COPY llm-lint /usr/local/bin/llm-lint
USER nonroot:nonroot
WORKDIR /workspace
ENTRYPOINT ["/usr/local/bin/llm-lint"]
CMD ["scan"]
