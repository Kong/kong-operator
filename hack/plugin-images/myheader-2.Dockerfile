FROM busybox:1.37.0@sha256:997af562a34229174bedf1251ca74617f6d94efaf47185b02729d18d2f029762 AS builder

COPY myheader /myheader/
RUN sed -i 's/"myheader"/"newheader"/g' /myheader/**
RUN sed -i 's/"roar"/"amazing"/g' /myheader/**


FROM scratch

COPY --from=builder /myheader /
