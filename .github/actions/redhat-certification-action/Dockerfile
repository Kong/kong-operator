FROM docker:29.9@sha256:08a98015fce594af7c382883bea080ea206de74402a95da9dc52b6428c8cd121

RUN apk add bash

COPY entrypoint.sh /entrypoint.sh

ENTRYPOINT ["/bin/bash", "-x", "/entrypoint.sh"]
