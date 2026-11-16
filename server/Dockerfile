# syntax=docker/dockerfile:1
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/miruro .

FROM alpine:3.22
RUN apk add --no-cache ca-certificates \
    && addgroup -S app \
    && adduser -S -G app app
WORKDIR /app
COPY --from=build /out/miruro /app/miruro
# Vercel routes to the container on PORT (set PORT=8080 in the project).
ENV ADDR=0.0.0.0:8080 PORT=8080
EXPOSE 8080
USER app
CMD ["/app/miruro"]
