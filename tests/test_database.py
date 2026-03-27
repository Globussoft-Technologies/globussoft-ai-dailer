import os
import sys
import pytest
import json
from unittest.mock import MagicMock, patch
from datetime import datetime, timedelta

sys.path.insert(0, os.path.abspath(os.path.join(os.path.dirname(__file__), "..")))

import database

@pytest.fixture
def mock_db():
    with patch("database.get_conn") as mock_get_conn:
        mock_conn = MagicMock()
        mock_cursor = MagicMock()
        
        # Configure standard cursor fetch behaviors to return empty dicts or lists
        mock_cursor.fetchone.return_value = {"cnt": 0, "status": "new", "id": 1, "name": "Fake Product", "test": "val", 'first_name': 'Test', 'org_name': 'Org', 'word': 'test', 'phonetic': 't'}
        mock_cursor.fetchall.return_value = [{"id": 1, "name": "Test Org", "credentials": '{"key": "val"}', "is_active": True, "last_synced_at": "", 'provider': 'hubspot', 'word': 'test', 'phonetic': 't'}]
        mock_cursor.rowcount = 1
        mock_cursor.lastrowid = 1
        
        mock_conn.cursor.return_value = mock_cursor
        mock_get_conn.return_value = mock_conn
        
        yield mock_cursor

def test_get_conn():
    # Attempt to cover line 19 by letting it run without mock, but mock pymysql.connect
    with patch("pymysql.connect") as mock_pymysql:
        database.get_conn()
        mock_pymysql.assert_called_once()

def test_database_initialization(mock_db):
    """Verify that init_db executes all table creation schemas natively without syntax errors."""
    database.init_db()
    
    # Collect all executed SQL command strings
    queries = [call[0][0] for call in mock_db.execute.call_args_list]
    
    assert any("CREATE TABLE IF NOT EXISTS leads" in q for q in queries)
    assert any("CREATE TABLE IF NOT EXISTS calls" in q for q in queries)
    assert any("CREATE TABLE IF NOT EXISTS call_transcripts" in q for q in queries)
    assert any("CREATE TABLE IF NOT EXISTS crm_integrations" in q for q in queries)

def test_init_db_integrity_error(mock_db):
    # Test the IntegrityError block during dummy lead insertion
    import pymysql
    def side_effect(*args, **kwargs):
        if "INSERT INTO leads" in args[0]:
            raise pymysql.err.IntegrityError("Duplicate")
        return None
    mock_db.execute.side_effect = side_effect
    database.init_db() # Should swallow error silently

def test_create_organization(mock_db):
    org_id = database.create_organization("Globussoft Demo")
    assert org_id == 1
    mock_db.execute.assert_called_with("INSERT INTO organizations (name) VALUES (%s)", ("Globussoft Demo",))

def test_get_all_organizations(mock_db):
    res = database.get_all_organizations()
    assert res[0]["name"] == "Test Org"

def test_delete_organization(mock_db):
    assert database.delete_organization(1) is True

def test_create_and_fetch_lead(mock_db):
    lead_id = database.create_lead({"first_name": "Test", "phone": "+9100"}, org_id=1)
    assert lead_id == 1
    
    database.get_all_leads(1)
    mock_db.execute.assert_called_with("SELECT * FROM leads WHERE org_id = %s ORDER BY id DESC", (1,))

def test_create_lead_no_interest(mock_db):
    mock_db.fetchone.return_value = {"name": "Test Product"}
    lead_id = database.create_lead({"first_name": "Test", "phone": "+9100"}, org_id=1)
    assert lead_id == 1

def test_search_leads(mock_db):
    database.search_leads("test", 1)
    mock_db.execute.assert_called()

def test_get_lead_by_id(mock_db):
    assert database.get_lead_by_id(1) is not None

def test_update_lead(mock_db):
    assert database.update_lead(1, {"first_name": "Updated"}, 1) is True

def test_delete_lead(mock_db):
    assert database.delete_lead(1, 1) is True

def test_call_status_updater(mock_db):
    database.log_call_status("+91000", "completed", "No errors")
    args = mock_db.execute.call_args[0]
    assert "UPDATE leads" in args[0]
    assert "SET status = %s" in args[0]
    assert args[1][0] == "Calling..."  # completed triggers Calling... logically
    assert "[20" in args[1][1]

def test_log_call_status_failed(mock_db):
    database.log_call_status("1234567890123", "failed", "Error msg") # test > 10 digit phone
    args = mock_db.execute.call_args[0]
    assert args[1][0] == "Call Failed (failed)"

def test_update_call_note(mock_db):
    mock_db.rowcount = 0
    database.update_call_note("sid", "note", "phone")
    assert mock_db.execute.call_count == 3 # Update, Insert fallback, Update lead
    
    # Test without phone
    mock_db.reset_mock()
    mock_db.rowcount = 1
    database.update_call_note("sid", "note")
    assert mock_db.execute.call_count == 1 

def test_get_all_sites(mock_db):
    assert database.get_all_sites(1) is not None

def test_create_punch(mock_db):
    assert database.create_punch("Agent", 1, 1.0, 1.0, "Valid") is True

def test_get_site_by_id(mock_db):
    assert database.get_site_by_id(1, 1) is not None

def test_update_lead_note(mock_db):
    assert database.update_lead_note(1, "note") is True

def test_update_lead_status_closed(mock_db):
    mock_db.fetchone.return_value = {"cnt": 0}
    assert database.update_lead_status(1, "Closed") is True
    # Verify tasks were created
    assert mock_db.execute.call_count > 2

def test_update_lead_status_warm(mock_db):
    mock_db.fetchone.side_effect = [{"cnt": 0}, {"first_name": "Test"}]
    assert database.update_lead_status(1, "Warm") is True
    assert mock_db.execute.call_count > 2

def test_get_all_tasks(mock_db):
    assert database.get_all_tasks(1) is not None

def test_complete_task(mock_db):
    assert database.complete_task(1) is True

def test_get_reports(mock_db):
    mock_db.fetchone.side_effect = [{"cnt": 10}, {"cnt": 5}, {"cnt": 8}, {"cnt": 2}]
    reports = database.get_reports(1)
    assert reports["total_leads"] == 10

def test_get_all_whatsapp_logs(mock_db):
    assert list(database.get_all_whatsapp_logs(1)) is not None

def test_upload_document(mock_db):
    assert database.upload_document(1, "file", "url") is True

def test_get_documents_by_lead(mock_db):
    assert database.get_documents_by_lead(1) is not None

def test_get_analytics(mock_db):
    mock_db.fetchone.side_effect = [{"cnt": 10}, {"cnt": 2}]
    stats = database.get_analytics()
    assert len(stats) == 7

def test_get_all_crm_integrations(mock_db):
    res = database.get_all_crm_integrations(1)
    assert res[0]["provider"] == "hubspot"
    
    # test parsing failure
    mock_db.fetchall.return_value = [{"id": 1, "credentials": "invalid", "is_active": True, "last_synced_at": "", 'provider': 'test'}]
    res = database.get_all_crm_integrations(1)
    assert res[0]["credentials"] == {}

def test_get_active_crm_integrations(mock_db):
    assert len(database.get_active_crm_integrations(1)) > 0
    assert len(database.get_active_crm_integrations()) > 0
    # test parsing failure
    mock_db.fetchall.return_value = [{"id": 1, "credentials": "invalid", "is_active": True, "last_synced_at": "", 'provider': 'test'}]
    res = database.get_active_crm_integrations()
    assert res[0]["credentials"] == {}

def test_save_crm_integration_existing(mock_db):
    mock_db.fetchone.return_value = {"id": 1}
    assert database.save_crm_integration("hubspot", {"key": "val"}, 1) is True
    
def test_save_crm_integration_new(mock_db):
    mock_db.fetchone.return_value = None
    assert database.save_crm_integration("hubspot", {"key": "val"}, 1) is True

def test_update_crm_last_synced(mock_db):
    assert database.update_crm_last_synced("hubspot", "time") is True

def test_create_user(mock_db):
    assert database.create_user("email@test", "hash", "name") == 1

def test_get_user_by_email(mock_db):
    assert database.get_user_by_email("test") is not None

def test_pronunciation(mock_db):
    assert database.get_all_pronunciations() is not None
    
    mock_db.fetchone.return_value = {"id": 1}
    assert database.add_pronunciation("word", "phonetic") is True
    mock_db.fetchone.return_value = None
    assert database.add_pronunciation("word2", "phonetic") is True
    
    assert database.delete_pronunciation(1) is True

def test_get_pronunciation_context_empty(mock_db):
    mock_db.fetchall.return_value = []
    assert database.get_pronunciation_context() == ""

def test_get_pronunciation_context_filled(mock_db):
    assert "bolna hai" in database.get_pronunciation_context()

def test_save_call_transcript(mock_db):
    transcript_json = '{"text": "Hello, how are you?"}'
    database.save_call_transcript(1, transcript_json, "https://exotel.recording.mp3", 45.5)
    
    mock_db.execute.assert_called_with(
        "INSERT INTO call_transcripts (lead_id, transcript, recording_url, call_duration_s) VALUES (%s, %s, %s, %s)",
        (1, transcript_json, "https://exotel.recording.mp3", 45.5)
    )

def test_get_transcripts_by_lead(mock_db):
    # test standard json return
    mock_db.fetchall.return_value = [{"id": 1, "lead_id":1, "transcript": {"test": "val"}, "recording_url": "", "call_duration_s": 0}]
    assert database.get_transcripts_by_lead(1)[0]["transcript"] == {"test": "val"}
    
    # test dict string return and parse
    mock_db.fetchall.return_value = [{"id": 1, "lead_id":1, "transcript": '{"test": "val"}', "recording_url": "", "call_duration_s": 0}]
    assert database.get_transcripts_by_lead(1)[0]["transcript"] == {"test": "val"}
    
    # test invalid json
    mock_db.fetchall.return_value = [{"id": 1, "lead_id":1, "transcript": 'invalid', "recording_url": "", "call_duration_s": 0}]
    assert database.get_transcripts_by_lead(1)[0]["transcript"] == 'invalid'

def test_product_lifecycle(mock_db):
    assert database.create_product(1, "Prod") == 1
    assert database.get_products_by_org(1) is not None
    assert database.update_product(1, name="New Name") is True
    assert database.delete_product(1) is True
    assert database.get_all_products() is not None

def test_get_product_knowledge_context(mock_db):
    mock_db.fetchall.return_value = [
        {"name": "Prod", "org_name": "Org", "scraped_info": "Info", "manual_notes": "Note", "org_id": 1}
    ]
    txt = database.get_product_knowledge_context(1)
    assert "Admin notes" in txt
    txt2 = database.get_product_knowledge_context()
    assert "Product: Prod" in txt2

def test_get_product_knowledge_context_empty(mock_db):
    mock_db.fetchall.return_value = []
    assert database.get_product_knowledge_context() == ""

def test_org_custom_prompt(mock_db):
    mock_db.fetchone.return_value = {"custom_system_prompt": "You are helpful."}
    assert database.get_org_custom_prompt(1) == "You are helpful."
    
    mock_db.fetchone.return_value = None
    assert database.get_org_custom_prompt(1) == ""
    
    mock_db.fetchone.return_value = {"custom_system_prompt": None}
    assert database.get_org_custom_prompt(1) == ""
    
    assert database.save_org_custom_prompt(1, "Prompt") is True

def test_org_voice_settings(mock_db):
    mock_db.fetchone.return_value = {"tts_provider": "smallest", "tts_voice_id": "v1", "tts_language": "en"}
    sett = database.get_org_voice_settings(1)
    assert sett["tts_provider"] == "smallest"
    
    mock_db.fetchone.return_value = None
    sett2 = database.get_org_voice_settings(1)
    assert sett2["tts_provider"] == "elevenlabs"
    
    mock_db.fetchone.return_value = {"tts_provider": None, "tts_voice_id": None, "tts_language": None}
    sett3 = database.get_org_voice_settings(1)
    assert sett3["tts_language"] == "hi"

    assert database.save_org_voice_settings(1, "dummy", "dummyid", "hi") is True

def test_knowledge_base(mock_db):
    assert database.log_knowledge_file(1, "test.pdf") == 1
    assert database.update_knowledge_file_status(1, "Active", 100) is True
    
    mock_db.fetchall.return_value = [{"id": 1, "filename": "test.pdf"}]
    assert len(database.get_knowledge_files(1)) == 1
    
    assert database.delete_knowledge_file(1, 1) is True
