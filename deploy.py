import subprocess

print("Launching OpenSSH Pipeline...")
cmd = [
    "ssh",
    "-i", r"C:\Users\Admin\.ssh\github-deploy-empcloud",
    "-o", "StrictHostKeyChecking=no",
    "empcloud-development@163.227.174.141",
    "cd ~/callified-ai && git pull origin main && pkill -f uvicorn ; sleep 3 ; mysql -u callified -pCallified@2026 callified_ai -N -B -e \"SELECT transcript FROM call_transcripts ORDER BY id DESC LIMIT 1\""
]

try:
    result = subprocess.run(cmd, capture_output=True, text=True, check=True)
    print("--- TRANSCRIPT LOGS ---")
    print(result.stdout.strip())
except subprocess.CalledProcessError as e:
    print("--- SSH ERROR ---")
    print(e.stderr.strip() if e.stderr else e.output)
