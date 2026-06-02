FROM docker:29.5@sha256:6b9cd914eb9c6b342c040a49a27a5eb3804453bae6ecc90f7ff96133595a95e8

RUN apk add bash

COPY entrypoint.sh /entrypoint.sh

ENTRYPOINT ["/bin/bash", "-x", "/entrypoint.sh"]
