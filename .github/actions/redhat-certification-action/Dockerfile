FROM docker:29.8@sha256:76cd6bbc3ab600fced21a7e1bea77ac00cb7c545eb95d5767e4ec4ffbcb242dc

RUN apk add bash

COPY entrypoint.sh /entrypoint.sh

ENTRYPOINT ["/bin/bash", "-x", "/entrypoint.sh"]
