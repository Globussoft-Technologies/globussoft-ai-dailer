import os
import re

filepath = r"c:\Users\Admin\gbs-projects\gbs-ai-dialer\frontend\src\App.jsx"
with open(filepath, 'r', encoding='utf-8') as f:
    content = f.read()

helper_code = """
  const apiFetch = async (url, options = {}) => {
    return fetch(url, {
      ...options,
      headers: {
        ...options.headers,
        'Authorization': `Bearer ${authToken}`
      }
    });
  };
"""

# Insert apiFetch if not exists
if "const apiFetch =" not in content:
    content = content.replace(
        "const [authToken, setAuthToken] = useState(localStorage.getItem('authToken') || '');",
        "const [authToken, setAuthToken] = useState(localStorage.getItem('authToken') || '');\n" + helper_code
    )

# Replace all fetch( EXCEPT for auth routes
lines = content.split('\n')
for i, line in enumerate(lines):
    if 'fetch(' in line and 'apiFetch' not in line and 'fetchLeads' not in line and 'fetchSites' not in line and 'fetchTasks' not in line and 'fetchReports' not in line and 'fetchWhatsappLogs' not in line and 'fetchAnalytics' not in line and 'fetchPronunciations' not in line and 'fetchOrgs' not in line and 'fetchOrgProducts' not in line:
        if '/auth/login' not in line and '/auth/signup' not in line and '/auth/me' not in line:
            lines[i] = re.sub(r'\bfetch\(', 'apiFetch(', line)

new_content = '\n'.join(lines)
with open(filepath, 'w', encoding='utf-8') as f:
    f.write(new_content)

print("Update complete")
