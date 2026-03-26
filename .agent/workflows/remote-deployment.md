---
description: Constraints for Remote SSH VPS Deployments
---
# System Infrastructure Constraint

Whenever you are asked to deploy code or interact with remote VPS/EC2 nodes over SSH, you MUST adhere to the following operational security constraints to prevent silent failures and architectural desyncs:

1. **NEVER trust stale deployment scripts in the repository.** 
   - Always use `systemctl list-units | grep [keyword]` to actively verify the exact name of the live service daemon running natively on the remote host before initiating any daemon restarts.
   
2. **Aggressively surface errors.** 
   - Always meticulously capture and print `stderr` alongside `stdout` in any remote Python paramiko/SSH execution scripts. Do not use generic string parsing that swallows exceptions (e.g., "Service Unit Not Found"). Silent failures must be violently surfaced.
   
3. **Verify Environment Telemetry.** 
   - Always run `cat .env` natively on the remote host to verify the physical environment variables (e.g. `PUBLIC_URL`) are populated correctly before assuming a Webhook or remote API failure is fundamentally a codebase bug.
