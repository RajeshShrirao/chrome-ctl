# Macro & Playbook Specification: Self-Learning Workflow Engine

**Document Version:** 1.0  
**Target Subsystem:** `internal/macro`  
**Storage Directory:** `~/.chrome-ctl/macros/<name>.json`  
**Primary Goal:** Deterministic local replay with **0 LLM token overhead**  

---

## 1. Engine Concept & Lifecycle

When an autonomous agent completes a multi-step task (e.g., submitting a business profile to a directory or sending a personalized outreach note), repeating that task via LLM reasoning burns 3,000–8,000 tokens each time.

The **Macro Engine** turns any interactive session into a local, compiled playbook:

```
[ Agent Explores & Acts via CLI ]
              |
              v (Records commands & DOM selectors)
`chrome-ctl macro start linkedin_outreach`
              |
[ Agent Executes: open -> click -> type -> click ]
              |
              v
`chrome-ctl macro save --params "profile_url,message_text"`
              |
              +---> Compiled to ~/.chrome-ctl/macros/linkedin_outreach.json
              |
[ Next 100 Outreach Tasks ]
              |
              v (0 LLM Tokens Consumed)
`chrome-ctl macro run linkedin_outreach --vars '{"profile_url":"...", "message_text":"..."}'`
```

---

## 2. Resilient Selector Hierarchy (Surviving DOM Refactors)

Single CSS selectors (like `.btn-primary` or auto-generated React class names like `css-175oi2r`) break easily when web apps deploy updates.

During recording, the Content Script captures a **Multi-Tier Resilient Target Descriptor** for every interacted element:

```
Tier 1: [data-testid] or [data-qa] (Immune to styling changes)
   |
   +---> Tier 2: ARIA Role + Accessible Name (e.g., button "Send Message")
            |
            +---> Tier 3: Stable Text Match (Exact or normalized string)
                     |
                     +---> Tier 4: Relative XPath (Scoped to parent container)
                              |
                              +---> Tier 5: Structural Hierarchy Path
```

At replay time, the resolver attempts Tier 1 through Tier 5 sequentially. If Tier 1 fails, it seamlessly falls back to Tier 2 without human or agent intervention.

---

## 3. Macro JSON Schema Specification

```json
{
  "$schema": "https://chrome-ctl.dev/schemas/macro-v1.json",
  "macro_version": "1.0",
  "name": "directory_business_registration",
  "description": "Registers a digital agency listing on Clutch/DesignRush directory.",
  "parameters": [
    {
      "name": "company_name",
      "type": "string",
      "description": "Legal business or brand name",
      "required": true
    },
    {
      "name": "website_url",
      "type": "string",
      "description": "Canonical company website URL",
      "required": true
    },
    {
      "name": "contact_email",
      "type": "string",
      "description": "Primary business contact email",
      "required": true
    },
    {
      "name": "logo_path",
      "type": "string",
      "description": "Absolute local path to brand logo image",
      "required": false,
      "default": ""
    }
  ],
  "steps": [
    {
      "step_id": 1,
      "description": "Navigate to registration portal",
      "action": "open",
      "url": "https://directory.example.com/get-listed",
      "wait_until": "networkidle",
      "timeout_ms": 15000
    },
    {
      "step_id": 2,
      "description": "Enter company name",
      "action": "type",
      "target": {
        "test_id": "company-name-input",
        "aria_label": "Company Name",
        "tag": "input",
        "type": "text",
        "css_selector": "form input[name='company']",
        "xpath": "//input[@name='company']"
      },
      "value": "{{company_name}}",
      "options": {
        "typo_rate": 0.01,
        "enter": false
      },
      "on_failure": "retry_3x"
    },
    {
      "step_id": 3,
      "description": "Enter company website",
      "action": "type",
      "target": {
        "test_id": "website-input",
        "aria_label": "Website URL",
        "tag": "input",
        "css_selector": "form input[name='website']"
      },
      "value": "{{website_url}}",
      "options": {
        "enter": false
      }
    },
    {
      "step_id": 4,
      "description": "Upload company logo",
      "action": "upload",
      "condition": "{{logo_path}} != ''",
      "target": {
        "tag": "input",
        "type": "file",
        "css_selector": "input[type='file']"
      },
      "value": "{{logo_path}}"
    },
    {
      "step_id": 5,
      "description": "Click Submit Registration",
      "action": "click",
      "target": {
        "aria_label": "Submit Registration",
        "text_match": "Submit Registration",
        "tag": "button"
      },
      "options": {
        "wait_after_ms": 3000
      },
      "on_failure": "prompt_fallback"
    }
  ]
}
```

---

## 4. Templating & Variable Interpolation

Values support Mustache syntax `{{param_name}}` with built-in sanitization transforms:

| Expression | Description | Example Output |
| :--- | :--- | :--- |
| `{{company_name}}` | Direct variable substitution | `"Apex Labs"` |
| `{{trim website_url}}` | Strips leading/trailing whitespace | `"https://apex.io"` |
| `{{lowercase email}}` | Forces lowercase characters | `"team@apex.io"` |
| `{{default logo_path "/tmp/default.png"}}` | Fallback if parameter is empty | `"/tmp/default.png"` |

---

## 5. Failure Handling & Agent Intervention Hook

When an automated step fails all 5 tiers of its resilient selector after 3 retry attempts:

1. **Local Daemon Pauses Execution:** It does not crash or kill the tab.
2. **State Snapshot Generated:** Daemon triggers `page.snap` on the current viewport.
3. **Structured Exit to Agent:**
   ```json
   {
     "status": "macro_step_failed",
     "macro": "directory_business_registration",
     "failed_step_id": 5,
     "reason": "Element 'Submit Registration' not found in DOM",
     "current_snapshot": "TAB [4] Registration | ...\n[12] <button:submit> \"Complete Signup\" @410,650"
   }
   ```
4. **Agent Self-Repair:** The AI agent reads the compact error, issues `chrome-ctl click 12`, and updates the macro definition with the new selector.
