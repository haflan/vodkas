FROM golang:alpine
WORKDIR /app
COPY . /app
RUN apk add git
RUN go get -d ./... ; \
    go build -o vodkas ;
   #go test
   
FROM alpine:latest
WORKDIR /root/
COPY --from=0 /app/vodkas .
EXPOSE 8080
CMD ["sh", "-c", "/root/vodkas -a $VADMINKEY -l $VSTORAGE"]
