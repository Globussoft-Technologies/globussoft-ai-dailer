import os
import sys
import pytest
from unittest.mock import patch, MagicMock

# Virtualize ML models globally
sys.modules['faiss'] = MagicMock()
sys.modules['sentence_transformers'] = MagicMock()
sys.modules['numpy'] = MagicMock()
sys.modules['fitz'] = MagicMock()

import rag

def test_paths():
    assert "org_1.index" in rag.get_index_path(1)
    assert "org_1_meta.json" in rag.get_metadata_path(1)

@patch("os.path.exists")
@patch("builtins.open")
@patch("json.load")
def test_load_index(mock_json, mock_open, mock_exists):
    mock_exists.return_value = False
    idx, meta = rag.load_index_and_meta(1)
    assert idx is None
    assert meta == []
    
    mock_exists.return_value = True
    rag.faiss.read_index.return_value = "IDX"
    mock_json.return_value = [{"test": "val"}]
    idx, meta = rag.load_index_and_meta(1)
    assert idx == "IDX"
    assert meta[0]["test"] == "val"

@patch("builtins.open")
@patch("json.dump")
def test_save_index(mock_dump, mock_open):
    rag.save_index_and_meta(1, "IDX", [{"test": "val"}])
    rag.faiss.write_index.assert_called_with("IDX", rag.get_index_path(1))

def test_chunk_text():
    res = rag.chunk_text("A" * 1000, 600, 100)
    assert len(res) == 2
    res2 = rag.chunk_text("A" * 100, 600, 100)
    assert len(res2) == 1

@patch("rag.load_index_and_meta")
@patch("rag.save_index_and_meta")
def test_ingest_pdf(mock_save, mock_load):
    import fitz
    import numpy as np
    
    # Mock PDF
    page_mock = MagicMock()
    page_mock.get_text.return_value = "Page 1 text"
    doc_mock = [page_mock, page_mock]
    fitz.open.return_value = doc_mock
    
    # Embeddings shape mock
    mock_arr = MagicMock()
    mock_arr.shape = (2, 384)
    np.array().astype.return_value = mock_arr
    
    mock_encode = MagicMock()
    mock_encode.astype.return_value = mock_arr
    rag.embedder.encode.return_value = mock_encode
    
    # 1. Existing index
    mock_load.return_value = (MagicMock(), [{"filename": "old", "text": "foo"}])
    res1 = rag.ingest_pdf("dummy.pdf", 1, "new.pdf")
    assert res1 > 0
    mock_save.assert_called()
    
    # 2. New index
    mock_load.return_value = (None, [])
    res2 = rag.ingest_pdf("dummy.pdf", 1, "new.pdf")
    assert res2 > 0

    # 3. Empty PDF
    with patch("rag.chunk_text", return_value=[]):
        res3 = rag.ingest_pdf("empty.pdf", 1, "empty.pdf")
        assert res3 == 0

@patch("rag.load_index_and_meta")
@patch("rag.save_index_and_meta")
@patch("os.path.exists")
@patch("os.remove")
def test_remove_file(mock_rem, mock_exists, mock_save, mock_load):
    # Missing index
    mock_load.return_value = (None, None)
    assert rag.remove_file_from_index("test.pdf", 1) is False
    
    # File not found in meta
    mock_load.return_value = ("IDX", [{"filename": "old.pdf"}])
    assert rag.remove_file_from_index("test.pdf", 1) is False
    
    # Full clear out
    mock_load.return_value = ("IDX", [{"filename": "test.pdf"}])
    mock_exists.return_value = True
    assert rag.remove_file_from_index("test.pdf", 1) is True
    assert mock_rem.call_count == 2
    
    # Partial clear out (re-embed)
    mock_load.return_value = ("IDX", [{"filename": "test.pdf"}, {"filename": "keep.pdf", "text": "keep"}])
    import numpy as np
    mock_arr = MagicMock()
    mock_arr.shape = (1, 384)
    np.array().astype.return_value = mock_arr
    assert rag.remove_file_from_index("test.pdf", 1) is True
    mock_save.assert_called()

@patch("rag.load_index_and_meta")
def test_retrieve_context(mock_load):
    mock_load.return_value = (None, None)
    assert rag.retrieve_context("q", 1) == ""
    
    import numpy as np
    mock_arr = MagicMock()
    np.array().astype.return_value = mock_arr
    
    # Mock return for query encoding
    mock_encode = MagicMock()
    mock_encode.astype.return_value = mock_arr
    rag.embedder.encode.return_value = mock_encode
    
    # Valid setup
    idx = MagicMock()
    idx.search.return_value = ([[0.1, 0.2]], [[0, -1]]) # index 0 valid, -1 invalid
    meta = [{"text": "Found it!"}]
    mock_load.return_value = (idx, meta)
    
    assert "Found it!" in rag.retrieve_context("q", 1)
    
    # Empty results
    idx.search.return_value = ([[0.1]], [[-1]])
    assert rag.retrieve_context("q", 1) == ""
