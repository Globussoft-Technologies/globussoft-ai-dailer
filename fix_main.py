import os

filepath = 'c:/Users/Admin/gbs-projects/gbs-ai-dialer/main.py'
with open(filepath, 'r', encoding='utf-8') as f:
    lines = f.readlines()

start_idx = -1
end_idx = -1
for i, line in enumerate(lines):
    if '# --- AUTHENTICATION & MOBILE APIS ---' in line:
        start_idx = i
        break

if start_idx != -1:
    for i in range(start_idx + 1, len(lines)):
        if '@app.post("/api/auth/signup")' in lines[i]:
            end_idx = i
            break

if start_idx != -1 and end_idx != -1:
    auth_block = lines[start_idx:end_idx]
    new_lines = lines[:start_idx] + lines[end_idx:]
    
    insert_idx = -1
    for i, line in enumerate(new_lines):
        if '@app.on_event("startup")' in line:
            insert_idx = i
            break
            
    if insert_idx != -1:
        final_lines = new_lines[:insert_idx] + ['\n\n'] + auth_block + ['\n\n'] + new_lines[insert_idx:]
        with open(filepath, 'w', encoding='utf-8') as f:
            f.writelines(final_lines)
        print("Successfully moved auth block before @app.on_event('startup')")
    else:
        print("startup event not found")
else:
    print("Auth block boundaries not found", start_idx, end_idx)
