FROM scratch

HEALTHCHECK --retries=10 CMD [ "/funds", "-url", "https://localhost:1080/health" ]

EXPOSE 1080
ENTRYPOINT [ "/funds" ]

COPY cacert.pem /etc/ssl/certs/ca-certificates.crt
COPY bin/funds /funds
