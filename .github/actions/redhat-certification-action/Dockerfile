FROM docker:29.5@sha256:7278248384185bcbb54c7a76b2f0a7d354c4aac920605cc1ab48968d961052fb

RUN apk add bash

COPY entrypoint.sh /entrypoint.sh

ENTRYPOINT ["/bin/bash", "-x", "/entrypoint.sh"]
