FROM busybox:1.37.0@sha256:997af562a34229174bedf1251ca74617f6d94efaf47185b02729d18d2f029762 AS builder

RUN mkdir myheader &&\
    dd if=/dev/urandom of=/myheader/handler.lua bs=512k count=1 &&\
    dd if=/dev/urandom of=/myheader/schema.lua bs=512k count=1


FROM scratch

COPY --from=builder /myheader /
