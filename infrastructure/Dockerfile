FROM alpine:3.16

ENV HOME /root

COPY ./bin/scan-queue-processor /scan-queue-processor

RUN mkdir $HOME/.kube

WORKDIR /

RUN chmod +x scan-queue-processor

CMD ["/scan-queue-processor"]
