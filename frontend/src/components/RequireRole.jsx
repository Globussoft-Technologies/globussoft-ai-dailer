import React from 'react';
import { Navigate } from 'react-router-dom';
import { useAuth } from '../contexts/AuthContext';

// RequireRole wraps a route element and redirects to /crm when the
// authenticated user's role isn't in the allowed list. The TopHeader already
// hides the corresponding nav buttons — this component blocks the matching
// case where a non-Admin types the URL directly.
//
// Pair this with the backend's `adminAuth` middleware (see
// backend/internal/api/middleware.go:requireRole). The frontend redirect
// improves UX; the server-side check is what actually enforces access.
export default function RequireRole({ allow = ['Admin'], children }) {
  const { currentUser } = useAuth();
  const role = currentUser?.role;
  if (!role || !allow.includes(role)) {
    return <Navigate to="/crm" replace />;
  }
  return children;
}
