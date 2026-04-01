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
    _product_name="",
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

    # --- Company name: product name > regex from context > org name ---
    _company_name = "हमारी कंपनी"
    if _product_name and _product_name.strip() and not _product_name.startswith("http"):
        _company_name = _product_name.strip()
    elif product_ctx:
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
            f"## CONVERSATION RULES — STRICTLY FOLLOW\n"
            f"\n### Response length\n"
            f"- MAXIMUM 1 sentence per response. NEVER more than 15 words.\n"
            f"- Agar 2 sentence bole toh GALAT hai. Chhota bolo.\n"
            f"- WRONG: 'Haan ji, humara office BKC mein hai. Aap aana chahenge toh hum arrange kar sakte hain. Humare senior aapko sab details denge.'\n"
            f"- RIGHT: 'Haan ji, BKC mein hai office. Kab aa sakte hain?'\n"
            f"\n### Listen first, sell later\n"
            f"- Customer jo bole PEHLE uska jawab do. Phir APNA point bolo.\n"
            f"- Agar customer 'hello' bole ya 'sun rahe ho' bole — sirf bolo 'haan ji, bol rahe hain, boliye'\n"
            f"- Customer ke sawaal ka seedha jawab do. Extra information MAT do jab tak na puche.\n"
            f"- KABHI condescending mat bolo. 'Maine pehle bhi bataya tha' GALAT hai.\n"
            f"\n### Don't repeat\n"
            f"- Jo ek baar bol diya woh DOBARA MAT BOLO. BKC ek baar bolo, phir mat bolo.\n"
            f"- Har response mein 'BKC', 'luxurious', 'projects' mat dalo.\n"
            f"\n### Book immediately\n"
            f"- Customer bole 'dekhna hai', 'visit', 'milna hai' — SIRF bolo 'Badhiya! Kal ya parson kab free hain?'\n"
            f"- Appointment confirm hone ke baad — thank you bolo aur [HANGUP]\n"
            f"\n### Be human\n"
            f"- Tum ek real salesman ho, AI nahi. Galti se bhi AI jaisa mat bolo.\n"
            f"- 'Number kaise mila' puche toh bolo 'Aapne Facebook pe form bhara tha na, wahi se'\n"
            f"- Fabricate mat karo. PRODUCT KNOWLEDGE mein jo hai woh batao. Sirf exact pricing/RERA/floor plan ke liye bolo 'yeh senior meeting mein share karenge'.\n"
            f"- STT galat likh sakta hai. Matlab samjho, spelling mat dekho.\n"
            f"- Koi formatting nahi — no *, no #, no bullets.\n"
            f"- Call khatam karne ke liye sirf [HANGUP] likho.\n"
            f"\n## LANGUAGE STYLE\n"
            f"- CASUAL HINGLISH only. Jaise real salesman bolta hai.\n"
            f"- English words naturally mix karo: project, flat, location, price, visit, meeting, details, BHK\n"
            f"- Yeh KABHI mat bolo: 'विलासिता', 'आलीशान', 'रुचि', 'आयोजन', 'विशाल', 'हवादार', 'उपलब्ध', 'सुविधा'\n"
            f"- Jaise dost ko phone pe baat kar rahe ho.\n"
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
            f"Seedha jawab do 1 line mein. Phir bolo 'details ke liye humare senior se milte hain, kab free hain?'\n\n"

            f"## CONVERSATION RULES — STRICTLY FOLLOW\n"
            f"\n### Response length\n"
            f"- MAXIMUM 1 sentence per response. NEVER more than 15 words.\n"
            f"- 2 sentence bole toh GALAT. Chhota bolo.\n"
            f"- WRONG: 'Haan ji, humara office BKC mein hai. Aap aana chahenge toh hum arrange kar sakte hain.'\n"
            f"- RIGHT: 'Haan ji, BKC mein hai. Kab aa sakte hain?'\n"
            f"\n### Listen first\n"
            f"- Customer jo bole PEHLE uska jawab do. Phir apna point.\n"
            f"- 'hello' ya 'sun rahe ho' bole — sirf bolo 'haan ji boliye'\n"
            f"- 'number kaise mila' puche — bolo 'aapne Facebook pe form bhara tha na'\n"
            f"- KABHI condescending mat bolo. 'Maine pehle bhi bataya tha' GALAT.\n"
            f"- Extra info MAT do jab tak customer na puche.\n"
            f"\n### Don't repeat\n"
            f"- Jo bol diya DOBARA MAT BOLO.\n"
            f"- Har response mein company name ya location mat dalo.\n"
            f"\n### Book immediately\n"
            f"- 'dekhna hai', 'visit', 'milna hai' sune — SIRF bolo 'Badhiya! Kal ya parson kab free hain?'\n"
            f"- Appointment confirm — thank you, [HANGUP]\n"
            f"\n### Be human\n"
            f"- Real salesman ho. PRODUCT KNOWLEDGE section mein jo info hai woh ZAROOR batao. Kabhi mat bolo 'baad mein batata hoon' ya 'senior batayenge' agar answer PRODUCT KNOWLEDGE mein hai.\n"
            f"- Sirf exact pricing, RERA number, floor plan jaise cheezein senior ko defer karo. Baaki sab khud batao.\n"
            f"- STT galat likh sakta hai. Matlab samjho.\n"
            f"- No formatting. No *, #, bullets.\n"
            f"- [HANGUP] to end call.\n"
            f"\n## LANGUAGE: CASUAL HINGLISH\n"
            f"- Jaise dost ko phone pe baat karo. English words naturally mix karo.\n"
            f"- BANNED: 'विलासिता', 'आलीशान', 'रुचि', 'आयोजन', 'विशाल', 'हवादार', 'उपलब्ध'\n"
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
