"""
prompt_builder.py — Builds the call context (system prompt, agent identity, greeting)
for the AI Dialer WebSocket handler.
"""
import re
from database import get_conn

# Voice ID → Hindi name mapping
_voice_names = {
    # Sarvam male
    'aditya': 'आदित्य', 'rahul': 'राहुल', 'amit': 'अमित', 'dev': 'देव', 'rohan': 'रोहन',
    'varun': 'वरुण', 'kabir': 'कबीर', 'manan': 'मनन', 'sumit': 'सुमित', 'ratan': 'रतन',
    'aayan': 'आयान', 'shubh': 'शुभ', 'ashutosh': 'आशुतोष', 'advait': 'अद्वैत',
    # Sarvam female
    'ritu': 'रितु', 'priya': 'प्रिया', 'neha': 'नेहा', 'pooja': 'पूजा', 'simran': 'सिमरन',
    'kavya': 'काव्या', 'ishita': 'इशिता', 'shreya': 'श्रेया', 'roopa': 'रूपा',
    # SmallestAI male
    'raj': 'राज', 'arnav': 'अर्णव', 'raman': 'रमन', 'raghav': 'राघव', 'aarav': 'आरव',
    'ankur': 'अंकुर', 'aravind': 'अरविंद', 'saurabh': 'सौरभ', 'chetan': 'चेतन', 'ashish': 'आशीष',
    # SmallestAI female
    'kajal': 'काजल', 'pragya': 'प्रज्ञा', 'nisha': 'निशा', 'deepika': 'दीपिका', 'diya': 'दिया',
    'sushma': 'सुषमा', 'shweta': 'श्वेता', 'ananya': 'अनन्या', 'mithali': 'मिताली',
    'saina': 'साइना', 'sanya': 'सान्या', 'mansi': 'मानसी',
}

_female_voices = {
    'kajal', 'pragya', 'nisha', 'deepika', 'diya', 'sushma', 'shweta', 'ananya',
    'mithali', 'saina', 'sanya', 'pooja', 'mansi', 'priya',
    'ritu', 'neha', 'simran', 'kavya', 'ishita', 'shreya', 'roopa',
    'amiAXapsDOAiHJqbsAZj', '6JsmTroalVewG1gA6Jmw', '9vP6R7VVxNwGIGLnpl17', 'hO2yZ8lxM3axUxL8OeKX',
}


def build_call_context(
    lead_name,
    lead_phone,
    interest,
    _call_lead_id,
    _campaign_id,
    _call_org_id,
    _tts_voice_override,
    product_ctx,
    _product_persona,
    _product_call_flow,
    pronunciation_ctx,
):
    """
    Build the full call context dict used by the WebSocket handler.

    Returns a dict with keys:
        dynamic_context, _agent_name, _lead_first, _company_name, _bol,
        _source_context, greeting_text
    """
    # --- Voice identity detection ---
    _voice_id = (_tts_voice_override or "").lower()
    _agent_name = _voice_names.get(_voice_id, "अर्जुन")
    if _voice_id in _female_voices:
        _agent_gender_hint = "तुम लड़की हो। 'रही हूँ', 'करूँगी', 'बोल रही हूँ' बोलो।"
        _bol = "बोल रही हूँ"
    else:
        _agent_gender_hint = "तुम लड़का हो। 'रहा हूँ', 'करूँगा', 'बोल रहा हूँ' बोलो।"
        _bol = "बोल रहा हूँ"

    # --- Extract company name from product context or org name ---
    _company_name = "हमारी कंपनी"
    if product_ctx:
        _co_match = re.search(r'by\s+(\w[\w\s]*?)[\)\—\-]', product_ctx)
        if _co_match:
            _company_name = _co_match.group(1).strip()
    if _company_name == "हमारी कंपनी":
        try:
            _org_conn = get_conn()
            _org_cur = _org_conn.cursor()
            _org_cur.execute("SELECT name FROM organizations WHERE id = %s", (_call_org_id if _call_org_id else 1,))
            _org_row = _org_cur.fetchone()
            if _org_row:
                _company_name = _org_row['name']
            _org_conn.close()
        except Exception:
            pass

    # --- Lead first name ---
    _lead_first = lead_name.split()[0] if lead_name.strip() else "Customer"

    # --- Lead source detection ---
    _lead_source = ""
    try:
        if _campaign_id:
            _src_conn = get_conn()
            _src_cur = _src_conn.cursor()
            _src_cur.execute("SELECT lead_source FROM campaigns WHERE id = %s", (_campaign_id,))
            _src_row = _src_cur.fetchone()
            if _src_row and _src_row.get('lead_source'):
                _lead_source = _src_row['lead_source'].strip().lower()
            _src_conn.close()
        if not _lead_source:
            _src_conn = get_conn()
            _src_cur = _src_conn.cursor()
            _src_cur.execute("SELECT source FROM leads WHERE id = %s", (_call_lead_id,))
            _src_row = _src_cur.fetchone()
            if _src_row:
                _lead_source = (_src_row.get('source') or "").strip().lower()
            _src_conn.close()
    except Exception:
        pass

    _source_map = {
        'meta': 'Facebook', 'facebook': 'Facebook', 'fb': 'Facebook',
        'google': 'Google', 'google ads': 'Google Ads',
        'instagram': 'Instagram', 'insta': 'Instagram',
        'linkedin': 'LinkedIn', 'website': 'हमारी वेबसाइट',
    }
    _platform = _source_map.get(_lead_source, "हमारी वेबसाइट")
    _source_context = (
        f"{_platform} पर हमारा ad देखकर enquiry की थी"
        if _platform != "हमारी वेबसाइट"
        else "हमारी वेबसाइट पर फॉर्म भरा था"
    )

    # --- Build system prompt (dynamic_context) ---
    if _product_persona or _product_call_flow:
        # Per-product prompt: product has its own persona + call flow
        dynamic_context = (
            f"{_product_persona}\n\n" if _product_persona else
            f"तुम {_agent_name} हो। {_agent_gender_hint} तुम {_company_name} कंपनी से बोल रहे हो।\n"
            f"तुम {_lead_first} को कॉल कर रहे हो। इन्होंने {_source_context}।\n"
            f"- लीड को सिर्फ पहले नाम से बुलाओ: '{_lead_first} जी'।\n\n"
        )
        # Inject variables into persona
        dynamic_context = dynamic_context.replace("{{first_name}}", _lead_first)
        dynamic_context = dynamic_context.replace("{{company}}", _company_name)
        dynamic_context = dynamic_context.replace("{{agent_name}}", _agent_name)
        dynamic_context = dynamic_context.replace("{{source_context}}", _source_context)

        if _product_call_flow:
            dynamic_context += f"\n## कॉल फ्लो\n{_product_call_flow}\n\n"
            dynamic_context = dynamic_context.replace("{{first_name}}", _lead_first)

        # Always append core rules
        dynamic_context += (
            f"## ज़रूरी नियम\n"
            f"- हर जवाब 1-2 लाइन। फोन कॉल है, छोटा बोलो। लेकिन अगर कस्टमर सवाल पूछे तो 2-3 लाइन में समझाओ।\n"
            f"- एक बार में एक ही सवाल।\n"
            f"- कस्टमर के सवालों का जवाब ज़रूर दो। कभी मत बोलो 'मैं नहीं बता सकता' या 'डीटेल में नहीं बता पाऊंगा'। जो जानकारी है उससे जवाब दो, फिर मीटिंग बुक करो।\n"
            f"- STT गलत लिख सकता है। मतलब समझो, spelling मत देखो।\n"
            f"- कोई formatting नहीं — no *, no #, no bullets। सादा टेक्स्ट।\n"
            f"- तुम {_agent_name} हो, इंसान हो। कभी AI मत बोलो।\n"
            f"- कॉल खत्म करने के लिए सिर्फ [HANGUP] लिखो।\n"
            f"{pronunciation_ctx}"
            f"{product_ctx}"
        )
    else:
        # Default hardcoded prompt (no per-product persona)
        dynamic_context = (
            f"तुम {_agent_name} हो। {_agent_gender_hint} तुम {_company_name} कंपनी से बोल रहे हो।\n"
            f"तुम {_lead_first} को कॉल कर रहे हो। इन्होंने {_source_context}।\n"
            f"- लीड को सिर्फ पहले नाम से बुलाओ: '{_lead_first} जी'। फुल नेम या लास्ट नेम कभी मत बोलो।\n\n"

            f"## तुम्हारी पहचान\n"
            f"- तुम्हारा नाम: {_agent_name}\n"
            f"- कंपनी: {_company_name}\n"
            f"- अगर कोई पूछे 'कहाँ से बोल रहे हो?' या 'कौन सी कंपनी?' तो तुरंत बोलो: 'मैं {_company_name} से {_agent_name} {_bol}।'\n"
            f"- कंपनी का नाम कभी छुपाओ मत। पहले ही बोल दो।\n\n"

            f"## गोल\n"
            f"मुख्य काम — अपॉइंटमेंट बुक करना। लेकिन अगर कस्टमर कोई सवाल पूछे तो पहले उसका जवाब दो, फिर मीटिंग बुक करो।\n\n"

            f"## कॉल फ्लो\n"
            f"1. इंट्रो: 'नमस्ते {_lead_first} जी, मैं {_agent_name}, {_company_name} से {_bol}। आपने {_source_context} क्या?'\n"
            f"2. अगर हाँ: 'अभी भी इंटरेस्ट है क्या इसमें?'\n"
            f"3. अगर इंटरेस्ट है: 'अच्छा बढ़िया, तो आप कल या परसों कब फ्री होंगे? हमारे सीनियर आपको कॉल करेंगे।'\n"
            f"4. टाइम मिलने पर: टाइम रिपीट करो, थैंक यू बोलो।\n"
            f"5. एंड: 'डन, आपको कॉल आएगा। थैंक यू!' फिर [HANGUP]\n\n"

            f"## अगर फॉर्म नहीं भरा\n"
            f"'अच्छा सॉरी, शायद गलती से नंबर आ गया। आपका दिन अच्छा हो।' फिर [HANGUP]\n\n"

            f"## अगर इंटरेस्ट नहीं\n"
            f"'बिल्कुल, कोई बात नहीं। थैंक यू।' फिर [HANGUP]\n\n"

            f"## अगर प्रोडक्ट के बारे में पूछें\n"
            f"जो जानकारी तुम्हारे पास है उससे जवाब दो — 2-3 लाइन में समझाओ। कभी मत बोलो 'मैं नहीं बता सकता' या 'डीटेल में नहीं समझा सकता'।\n"
            f"जवाब देने के बाद बोलो: 'और ज़्यादा जानकारी के लिए हमारे सीनियर आपसे बात करेंगे, आप कब फ्री हैं?'\n\n"

            f"## ज़रूरी नियम\n"
            f"- हर जवाब 1-2 लाइन। फोन कॉल है, छोटा बोलो।\n"
            f"- एक बार में एक ही सवाल। दो चीज़ें एक साथ मत बोलो।\n"
            f"- सवाल में 'क्या', 'ना', 'है क्या' लगाओ ताकि सवाल जैसा लगे।\n"
            f"- STT गलत लिख सकता है ('की थी' → 'key thi')। मतलब समझो, spelling मत देखो।\n"
            f"- कोई formatting नहीं — no *, no #, no bullets। सादा टेक्स्ट।\n"
            f"- तुम {_agent_name} हो, इंसान हो। कभी AI मत बोलो।\n"
            f"- जो बोल चुके हो दोबारा मत बोलो।\n"
            f"- कॉल खत्म करने के लिए सिर्फ [HANGUP] लिखो।\n"
            f"{pronunciation_ctx}"
            f"{product_ctx}"
        )

    greeting_text = f"नमस्ते {_lead_first} जी, मैं {_agent_name}, {_company_name} से {_bol}। आपने {_source_context} क्या?"

    return {
        "dynamic_context": dynamic_context,
        "_agent_name": _agent_name,
        "_lead_first": _lead_first,
        "_company_name": _company_name,
        "_bol": _bol,
        "_source_context": _source_context,
        "greeting_text": greeting_text,
    }
