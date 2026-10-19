FROM golang:1.26.1-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY main.go ./
COPY analyzer ./analyzer
COPY web ./web
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/govisualize .

FROM alpine:3.23 AS runtime
RUN apk add --no-cache ca-certificates git \
    && addgroup -S govisualize \
    && adduser -S -G govisualize govisualize
COPY --from=build /out/govisualize /usr/local/bin/govisualize
USER govisualize
ENV GOVISUALIZE_API_ADDR=0.0.0.0:8080
EXPOSE 8080
ENTRYPOINT ["govisualize"]
