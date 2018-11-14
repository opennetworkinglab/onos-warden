FROM scratch
#ADD ca-certificates.crt /etc/ssl/certs/
ADD server /
CMD ["/server"]

# How to build server:
#CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o server server.go
