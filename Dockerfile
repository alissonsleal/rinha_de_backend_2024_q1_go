FROM golang:1.22

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY ./src ./

RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/rinha

EXPOSE 8080

# copy wait-for-it.sh to the /app directory and make it executable
COPY wait-for-it.sh /app
RUN chmod +x /app/wait-for-it.sh

# use wait-for-it.sh to wait for the database to be ready, then start the app
# CMD ["/bin/rinha"]
CMD ["./wait-for-it.sh", "db:5432", "--", "/bin/rinha"]