FROM golang:1.27.1 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG BUILD_TIME
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.buildTime=${BUILD_TIME}" -o /out/risk ./cmd/risk

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/risk /risk
USER nonroot:nonroot
EXPOSE 4000
ENTRYPOINT ["/risk"]
