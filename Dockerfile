FROM golang:1.26

# Set destination for COPY
WORKDIR /go/src/app

# Download Go modules
COPY go.mod go.sum ./
RUN go mod download
