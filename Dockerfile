FROM scratch

COPY elector .

ENTRYPOINT ["./elector"]