import { useEffect, useState } from "react";
import { buildControlPlaneUrl, getStoredAuthToken } from "@mspace/core";

export function isIssueAttachmentPath(src: string) {
  return src.startsWith("/api/attachments/");
}

export function useResolvedIssueImageSrc(rawSrc: string) {
  const src = rawSrc.trim();
  const token = getStoredAuthToken();
  const isAttachment = isIssueAttachmentPath(src);
  const [resolvedSrc, setResolvedSrc] = useState(isAttachment ? "" : src);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!isAttachment) {
      setResolvedSrc(src);
      setError("");
      return;
    }
    if (!token) {
      setResolvedSrc("");
      setError("missing authorization");
      return;
    }

    const controller = new AbortController();
    let objectUrl = "";
    setResolvedSrc("");
    setError("");

    fetch(buildControlPlaneUrl(src), {
      headers: { Authorization: `Bearer ${token}` },
      signal: controller.signal,
    })
      .then(async (response) => {
        if (!response.ok) {
          throw new Error((await response.text()) || `Request failed with status ${response.status}`);
        }
        return response.blob();
      })
      .then((blob) => {
        if (controller.signal.aborted) return;
        objectUrl = URL.createObjectURL(blob);
        setResolvedSrc(objectUrl);
      })
      .catch((cause: unknown) => {
        if (controller.signal.aborted) return;
        setError(cause instanceof Error ? cause.message : "image unavailable");
      });

    return () => {
      controller.abort();
      if (objectUrl) URL.revokeObjectURL(objectUrl);
    };
  }, [isAttachment, src, token]);

  return {
    src: resolvedSrc,
    loading: isAttachment && !resolvedSrc && !error,
    error,
    isAttachment,
  };
}
