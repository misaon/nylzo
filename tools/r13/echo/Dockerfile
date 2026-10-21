FROM --platform=$BUILDPLATFORM golang:1.27 AS build
WORKDIR /src
COPY go.mod ./
COPY *.go ./
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/r13-echo .

FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /out/r13-echo /r13-echo
ENV PORT=8080
EXPOSE 8080
ENTRYPOINT ["/r13-echo"]
