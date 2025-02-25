FROM python:3

RUN echo {{ .Values.foo.bar }}
{{- if .Values.extra }}
RUN echo {{ .Values.extra.value }}
{{ end }}