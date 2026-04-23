"""
Callified RAG Microservice — Phase 5
Thin FastAPI wrapper around rag.py.

Routes:
  GET  /health               liveness probe
  GET  /retrieve             semantic search (query, org_id, top_k)
  POST /ingest               PDF upload + FAISS index update
  DELETE /knowledge          remove a file from an org's index
"""
import os
import tempfile

from fastapi import FastAPI, File, HTTPException, Query, UploadFile

import rag  # rag.py sits alongside this file

app = FastAPI(title="Callified RAG Service", version="1.0.0")


@app.get("/health")
def health():
    return {"status": "ok", "service": "rag"}


@app.get("/retrieve")
def retrieve(
    query: str = Query(..., description="Search query from the LLM pipeline"),
    org_id: int = Query(..., description="Organisation ID"),
    top_k: int = Query(3, ge=1, le=20, description="Max chunks to return"),
):
    """Return the top-k relevant context chunks for the given query and org."""
    context = rag.retrieve_context(query=query, org_id=org_id, top_k=top_k)
    return {"context": context}


@app.post("/ingest")
async def ingest(
    file: UploadFile = File(...),
    org_id: int = Query(..., description="Organisation ID"),
):
    """Accept a PDF upload and add its chunks to the org FAISS index."""
    if not file.filename or not file.filename.lower().endswith(".pdf"):
        raise HTTPException(status_code=400, detail="only PDF files are supported")

    data = await file.read()
    with tempfile.NamedTemporaryFile(delete=False, suffix=".pdf") as tmp:
        tmp.write(data)
        tmp_path = tmp.name

    try:
        chunks = rag.ingest_pdf(filepath=tmp_path, org_id=org_id, filename=file.filename)
    finally:
        os.unlink(tmp_path)

    return {"filename": file.filename, "chunks_indexed": chunks}


@app.delete("/knowledge")
def delete_knowledge(
    org_id: int = Query(..., description="Organisation ID"),
    filename: str = Query(..., description="Filename to remove from index"),
):
    """Remove all chunks for a file from the org FAISS index."""
    removed = rag.remove_file_from_index(filename=filename, org_id=org_id)
    return {"removed": removed}
