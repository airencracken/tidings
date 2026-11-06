FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /tidings ./cmd/tidings

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /tidings /tidings
ENV TIDINGS_DATA_DIR=/data TIDINGS_ADDR=0.0.0.0:8083
VOLUME ["/data"]
EXPOSE 8083
ENTRYPOINT ["/tidings"]
CMD ["serve"]
