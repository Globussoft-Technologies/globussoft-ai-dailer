# Callified AI: Database Schema

This document dictates the core schema used across all environments. The project uses MySQL (via `pymysql`) natively inside `database.py`. The central `init_db()` function establishes these schemas at boot.

## Core Tables

### 1. `organizations`
Multi-tenant core table tracking different organizational branches or distinct companies.
- `id` (INT PK)
- `name` (VARCHAR)
- `created_at` (TIMESTAMP)
- `custom_system_prompt` (TEXT) - Optional LLM override prompt per organization.
- `tts_provider` (VARCHAR) - ElevenLabs / SmallestAI.
- `tts_voice_id` (VARCHAR)
- `tts_language` (VARCHAR)

### 2. `users`
Portal admins and agents attached to Organizations.
- `id` (INT PK)
- `org_id` (INT FK)
- `email` (VARCHAR UNIQUE)
- `password_hash` (VARCHAR)
- `full_name` (VARCHAR)
- `role` (VARCHAR) - Usually 'Admin' or 'Agent'.

### 3. `leads`
The core customer data target for the AI dialer.
- `id` (INT PK)
- `org_id` (INT FK)
- `first_name` (VARCHAR)
- `last_name` (VARCHAR)
- `phone` (VARCHAR UNIQUE)
- `source` (VARCHAR)
- `status` (VARCHAR) - Example: 'new', 'Calling...', 'Warm', 'Closed'.
- `follow_up_note` (TEXT) - Generated automatically by Gemini 2.5 after AI analyzes transcript.
- `external_id` (VARCHAR) - Foreign mapping to Hubspot/Salesforce.
- `crm_provider` (VARCHAR) - Tag identifying original CRM source.

### 4. `calls` & `call_transcripts`
Tracks telecom states and Deepgram dialogue arrays.
- **calls**: Tracks individual telecom pushes (`lead_id`, `call_sid`, `status`).
- **call_transcripts**:
  - `lead_id` (INT FK)
  - `transcript` (JSON) - Deepgram conversation array.
  - `recording_url` (TEXT)
  - `call_duration_s` (FLOAT)

### 5. `products`
Products, services, or inventory items sold by the Organization. Ingested dynamically by the Gemini payload over `get_product_knowledge_context()`.
- `id` (INT PK)
- `org_id` (INT FK)
- `name` (VARCHAR) - e.g. "Green Valley Project"
- `website_url` (TEXT)
- `scraped_info` (LONGTEXT)
- `manual_notes` (LONGTEXT)

### 6. `sites` & `punches`
Geofencing Field Operations Engine.
- **sites**: Physical locations with `lat`/`lon` targets.
- **punches**: Agent check-ins containing computed haversine distance `status` (Valid/Invalid).

### 7. `tasks`
Internal Kanban tasks dynamically generated when a lead goes `Closed` (e.g. Legal, Accounts).
- `lead_id` (INT FK)
- `department` (VARCHAR)
- `description` (TEXT)
- `status` (VARCHAR)

### 8. `whatsapp_logs` & `documents`
- **whatsapp_logs**: Tracks automated WhatsApp nudges (e.g., e-brochures triggered by 'Warm' status).
- **documents**: File paths for Lead KYC uploads (Aadhar/PAN).

### 9. `crm_integrations`
Holds dynamically encrypted 3rd-party CRM tokens (Hubspot, Salesforce, Zoho).
- `org_id` (INT FK)
- `provider` (VARCHAR)
- `credentials` (TEXT JSON)
- `is_active` (BOOLEAN)
- `last_synced_at` (VARCHAR)

### 10. `pronunciation_guide`
Global phonetic mappings injected into Gemini 2.5 logic to force specific pronunciations for regional Indian words.
- `word` (VARCHAR UNIQUE)
- `phonetic` (VARCHAR)
