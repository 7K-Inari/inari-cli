FROM golang:1.27.4-bookworm@sha256:8c4f7ee39e95f12739404475e5be21f2be462ddeb4470eb0f88f5edbd330a375 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/__HIROBA_NAME__ ./cmd/__HIROBA_NAME__

FROM gcr.io/distroless/static-debian13:nonroot@sha256:89e00a7e78df216bcbf495e7f43135f03ddbd3e98024a4104731872d6c8cf3c6
COPY --from=build /out/__HIROBA_NAME__ /usr/local/bin/__HIROBA_NAME__
ENV SCAFFOLD_HTTP=true
EXPOSE 8080
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/__HIROBA_NAME__"]
