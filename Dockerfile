# syntax=docker/dockerfile:1

# ---- Stage 1: build the Next.js UI into web/out ----
FROM node:22-bookworm-slim AS ui
WORKDIR /app/web
COPY web/package.json web/package-lock.json* ./
RUN npm ci || npm install
COPY web/ ./
# Public origin baked into SEO tags (canonical URL, sitemap, OG/Twitter image
# URLs). Set it to your deployed domain so Google indexes the right host:
#   docker build --build-arg NEXT_PUBLIC_SITE_URL=https://conduit.yourco.com .
ARG NEXT_PUBLIC_SITE_URL
ENV NEXT_PUBLIC_SITE_URL=${NEXT_PUBLIC_SITE_URL}
RUN npm run build

# ---- Stage 2: build the Go binary with the UI embedded ----
FROM golang:1.24-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Bring in the freshly built export so //go:embed all:web/out has content.
COPY --from=ui /app/web/out ./web/out
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /conduitd .

# ---- Stage 3: minimal runtime ----
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /conduitd /conduitd
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/conduitd"]
