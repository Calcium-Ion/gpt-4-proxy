FROM golang:1.20-alpine

WORKDIR /app
COPY go_build_gpt_4_proxy_linux gpt-4-proxy

#RUN go build

EXPOSE 3800
CMD [ "/app/gpt-4-proxy" ]