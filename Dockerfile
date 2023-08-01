FROM golang:1.20-alpine

WORKDIR /app
COPY . .

RUN go build

EXPOSE 3800
CMD [ "/app/gpt-4-proxy" ]