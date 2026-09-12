# docker build -f services/ingestion-silent-diff/Dockerfile -t soteria/ingestion-silent-diff .
FROM python:3.12-slim
ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1 PIP_NO_CACHE_DIR=1
WORKDIR /app
COPY services/ingestion-silent-diff/requirements.txt ./
RUN pip install -r requirements.txt
COPY services/ingestion-silent-diff/silentdiff ./silentdiff
COPY services/ingestion-silent-diff/sources.yaml services/ingestion-silent-diff/fixtures ./
RUN mkdir -p /data && useradd -r -u 10001 silentdiff && chown silentdiff /data
USER silentdiff
ENV DB_PATH=/data/silent-diff.db PORT=8085 SOURCES=/app/sources.yaml
VOLUME ["/data"]
EXPOSE 8085
ENTRYPOINT ["python", "-m", "silentdiff"]
CMD ["run"]
