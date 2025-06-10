FROM golang:bookworm
WORKDIR /opt/app
COPY . .
RUN chmod 775 /opt/app
EXPOSE 8080


CMD ["go", "run", "src/main.go"]
