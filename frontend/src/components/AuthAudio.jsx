import React, { useEffect, useState } from 'react';
import { useAuth } from '../contexts/AuthContext';

// AuthAudio renders an <audio> element whose source is loaded as a blob via
// the apiFetch wrapper (Authorization header) instead of being attached as
// `?token=…` in the URL. The long-lived JWT must never appear in URLs because
// reverse proxies, browser history and Referer headers leak query strings.
// (issue #80)
//
// External URLs (anything not starting with /api/recordings/) are passed
// through unchanged — they're public/Exotel-hosted and don't need our auth.
export default function AuthAudio({ src, ...props }) {
  const { apiFetch } = useAuth();
  const [blobUrl, setBlobUrl] = useState(null);

  useEffect(() => {
    if (!src) { setBlobUrl(null); return; }
    if (!src.startsWith('/api/recordings/')) { setBlobUrl(src); return; }

    let cancelled = false;
    let createdUrl = null;
    apiFetch(src)
      .then(r => r.ok ? r.blob() : Promise.reject(new Error(`audio fetch ${r.status}`)))
      .then(blob => {
        if (cancelled) return;
        createdUrl = URL.createObjectURL(blob);
        setBlobUrl(createdUrl);
      })
      .catch(() => { if (!cancelled) setBlobUrl(null); });

    return () => {
      cancelled = true;
      if (createdUrl) URL.revokeObjectURL(createdUrl);
    };
  }, [src, apiFetch]);

  if (!blobUrl) return <audio controls {...props} />;
  return <audio controls src={blobUrl} {...props} />;
}
