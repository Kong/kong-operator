FROM busybox:1.38.0@sha256:79b6957454b0506796c042e9e2dfedd996c7efc93e701166d2dc9569bc9cc096 AS builder

COPY myheader /myheader/
RUN sed -i 's/"myheader"/"newheader"/g' /myheader/**
RUN sed -i 's/"roar"/"amazing"/g' /myheader/**


FROM scratch

COPY --from=builder /myheader /
