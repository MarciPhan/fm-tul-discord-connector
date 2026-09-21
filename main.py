import os
import uuid
import time
import asyncio
import threading
from typing import Optional, Dict

import httpx
from dotenv import load_dotenv
from fastapi import FastAPI, Request, HTTPException, Query
from fastapi.responses import RedirectResponse, HTMLResponse
import disnake
from disnake.ext import commands

# Nacteni promennych z .env souboru
load_dotenv()

# --- KONFIGURACE Z PROSTREDI ---
MICROSOFT_CLIENT_ID = os.getenv("MICROSOFT_CLIENT_ID", "")
MICROSOFT_CLIENT_SECRET = os.getenv("MICROSOFT_CLIENT_SECRET", "")
MICROSOFT_TENANT_ID = os.getenv("MICROSOFT_TENANT_ID", "common")
REDIRECT_URI = os.getenv("REDIRECT_URI", "http://localhost:8000/callback")

DISCORD_TOKEN = os.getenv("DISCORD_TOKEN", "")
GUILD_ID = int(os.getenv("GUILD_ID", "0")) if os.getenv("GUILD_ID", "0").isdigit() else 0
FM_STUDENT_ROLE_ID = int(os.getenv("FM_STUDENT_ROLE_ID", "0")) if os.getenv("FM_STUDENT_ROLE_ID", "0").isdigit() else 0
FM_STAFF_ROLE_ID = int(os.getenv("FM_STAFF_ROLE_ID", "0")) if os.getenv("FM_STAFF_ROLE_ID", "0").isdigit() else 0

APP_HOST = os.getenv("HOST", "0.0.0.0")
APP_PORT = int(os.getenv("PORT", "8000"))

# Microsoft Identity Platform (v2.0) endpointy
MS_AUTHORIZE_URL = f"https://login.microsoftonline.com/{MICROSOFT_TENANT_ID}/oauth2/v2.0/authorize"
MS_TOKEN_URL = f"https://login.microsoftonline.com/{MICROSOFT_TENANT_ID}/oauth2/v2.0/token"
MS_GRAPH_ME_URL = "https://graph.microsoft.com/v1.0/me"

# Docasne uloziste pro overovaci session: state_token -> { discord_id, created_at }
verification_states: Dict[str, dict] = {}

# Inicializace FastAPI a Discord Bota
app = FastAPI(title="FM TUL Discord Bridge", version="2.0.0")
bot = commands.Bot(command_prefix="!", intents=disnake.Intents.all())


# --- DISCORD BOT LOGIKA ---

@bot.event
async def on_ready():
    print(f"✅ Discord Bot {bot.user} je pripojen a pripraven!")
    if GUILD_ID:
        guild = bot.get_guild(GUILD_ID)
        if guild:
            print(f"🏰 Pripojeno k serveru: {guild.name} (ID: {guild.id})")
        else:
            print(f"⚠️ Server s GUILD_ID={GUILD_ID} nebyl nalezen. Zkontrolujte ID.")

@bot.slash_command(name="overit", description="Získejte ověření pro studenty a zaměstnance FM TUL")
async def slash_overit(inter: disnake.ApplicationCommandInteraction):
    """Vygeneruje jednorazovy odkaz pro propojeni uctu s TUL identitou."""
    state_token = str(uuid.uuid4())
    verification_states[state_token] = {
        "discord_id": inter.author.id,
        "created_at": time.time()
    }
    
    base_url = REDIRECT_URI.rsplit("/callback", 1)[0]
    verify_url = f"{base_url}/login?state={state_token}"
    
    embed = disnake.Embed(
        title="🎓 Ověření identity FM TUL",
        description=(
            f"Ahoj {inter.author.mention},\n\n"
            "Pro získání přístupu do fakultních kanálů FM se prosím přihlas svým univerzitním "
            "**Microsoft účtem TUL** (`@tul.cz`).\n\n"
            "Klikni na tlačítko níže pro zahájení přihlášení."
        ),
        color=disnake.Color.blue()
    )
    embed.set_footer(text="Odkaz je platný 15 minut a je určen pouze pro vás.")
    
    view = disnake.ui.View()
    view.add_item(disnake.ui.Button(label="Přihlásit se přes TUL", url=verify_url, style=disnake.ButtonStyle.link))
    
    await inter.response.send_message(embed=embed, view=view, ephemeral=True)

@bot.command(name="overit")
async def prefix_overit(ctx: commands.Context):
    """Zalozni prefixovy prikaz !overit pro pripad, ze uzivatel nepouzije slash command."""
    state_token = str(uuid.uuid4())
    verification_states[state_token] = {
        "discord_id": ctx.author.id,
        "created_at": time.time()
    }
    
    base_url = REDIRECT_URI.rsplit("/callback", 1)[0]
    verify_url = f"{base_url}/login?state={state_token}"
    
    try:
        await ctx.author.send(
            f"Ahoj! Zde je tvůj privátní odkaz pro ověření identity FM TUL:\n{verify_url}\n"
            "Odkaz je platný 15 minut."
        )
        if ctx.guild:
            await ctx.message.reply("Poslal jsem ti odkaz do soukromé zprávy! 📩", delete_after=10)
    except disnake.Forbidden:
        await ctx.reply("Nemohl jsem ti poslat soukromou zprávu. Povol si prosím příjem DM zpráv od členů serveru.")


async def assign_discord_role(discord_id: int, user_info: dict) -> dict:
    """Priradi roli na Discordu a pripadne zaktualizuje prezdivku."""
    if not GUILD_ID:
        return {"success": False, "message": "GUILD_ID neni nakonfigurovano."}
    
    guild = bot.get_guild(GUILD_ID)
    if not guild:
        return {"success": False, "message": f"Server s ID {GUILD_ID} nebyl nalezen."}
    
    member = guild.get_member(discord_id)
    if not member:
        try:
            member = await guild.fetch_member(discord_id)
        except Exception:
            return {"success": False, "message": "Uživatel zatím není na Discord serveru."}
    
    # Urceni spravne role (zamestnanec vs. student)
    role_to_add = None
    role_name = "Ověřený člen FM"
    
    is_staff = user_info.get("is_staff", False)
    if is_staff and FM_STAFF_ROLE_ID:
        role_to_add = guild.get_role(FM_STAFF_ROLE_ID)
        role_name = "Zaměstnanec FM"
    elif FM_STUDENT_ROLE_ID:
        role_to_add = guild.get_role(FM_STUDENT_ROLE_ID)
        role_name = "Student FM"
    
    assigned_roles = []
    if role_to_add:
        try:
            await member.add_roles(role_to_add, reason="Ověření přes Microsoft TUL účet")
            assigned_roles.append(role_to_add.name)
        except disnake.Forbidden:
            print(f"⚠️ Bot nema opravneni priradit roli {role_to_add.name}. Zkontrolujte hierarchii roli.")
    
    # Volitelna aktualizace prezdivky na Discordu na realne jmeno
    real_name = user_info.get("name")
    nickname_changed = False
    if real_name:
        try:
            await member.edit(nick=real_name, reason="Synchronizace jména z TUL identit")
            nickname_changed = True
        except Exception:
            pass # Bezne u vlastnika serveru nebo kvuli chybejicim pravum
            
    return {
        "success": True,
        "member_name": str(member),
        "role_name": role_name,
        "nickname_changed": nickname_changed
    }


# --- FASTAPI WEB ENDPOINTY ---

@app.get("/", response_class=HTMLResponse)
async def home(request: Request, state: Optional[str] = None):
    """Hlavni prehledova stranka s modernim designem."""
    bot_status = "Online 🟢" if bot.is_ready() else "Offline / Nenakonfigurován 🟡"
    guild_name = bot.get_guild(GUILD_ID).name if (bot.is_ready() and GUILD_ID and bot.get_guild(GUILD_ID)) else "Nenastaveno"
    
    login_href = f"/login?state={state}" if state else "/login"
    mock_href = f"/mock-login?state={state}" if state else "/mock-login"
    
    return f"""
    <!DOCTYPE html>
    <html lang="cs">
    <head>
        <meta charset="UTF-8">
        <meta name="viewport" content="width=device-width, initial-scale=1.0">
        <title>FM TUL Discord Bridge</title>
        <link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700&display=swap" rel="stylesheet">
        <style>
            * {{ margin: 0; padding: 0; box-sizing: border-box; font-family: 'Inter', sans-serif; }}
            body {{ background: #0f172a; color: #f8fafc; display: flex; align-items: center; justify-content: center; min-height: 100vh; padding: 20px; }}
            .card {{ background: #1e293b; border: 1px solid #334155; border-radius: 16px; width: 100%; max-width: 540px; padding: 36px; box-shadow: 0 20px 25px -5px rgba(0,0,0,0.4); }}
            .badge {{ display: inline-block; padding: 4px 12px; border-radius: 9999px; font-size: 13px; font-weight: 600; margin-bottom: 12px; background: #0284c7; color: white; }}
            h1 {{ font-size: 26px; font-weight: 700; margin-bottom: 12px; }}
            p.desc {{ color: #94a3b8; font-size: 15px; line-height: 1.5; margin-bottom: 24px; }}
            .btn {{ display: flex; align-items: center; justify-content: center; gap: 10px; width: 100%; padding: 14px 20px; border-radius: 10px; font-size: 16px; font-weight: 600; text-decoration: none; cursor: pointer; transition: all 0.2s; border: none; }}
            .btn-ms {{ background: #2563eb; color: white; margin-bottom: 12px; }}
            .btn-ms:hover {{ background: #1d4ed8; transform: translateY(-1px); }}
            .btn-mock {{ background: #334155; color: #e2e8f0; }}
            .btn-mock:hover {{ background: #475569; }}
            .status-box {{ margin-top: 28px; padding: 16px; background: #0f172a; border-radius: 10px; border: 1px solid #1e293b; font-size: 13px; color: #94a3b8; }}
            .status-item {{ display: flex; justify-content: space-between; margin-bottom: 6px; }}
            .status-item:last-child {{ margin-bottom: 0; }}
            .status-value {{ color: #f1f5f9; font-weight: 500; }}
        </style>
    </head>
    <body>
        <div class="card">
            <span class="badge">FM TUL • Ověření identity</span>
            <h1>Fakultní Discord Bridge</h1>
            <p class="desc">
                Pro získání přístupu do interních studentských kanálů Fakulty mechatroniky se přihlaste svým univerzitním Microsoft účtem TUL.
            </p>
            
            <a href="{login_href}" class="btn btn-ms">
                <svg width="20" height="20" viewBox="0 0 21 21" fill="none" xmlns="http://www.w3.org/2000/svg">
                    <rect x="1" y="1" width="9" height="9" fill="#f25022"/>
                    <rect x="11" y="1" width="9" height="9" fill="#7fba00"/>
                    <rect x="1" y="11" width="9" height="9" fill="#00a4ef"/>
                    <rect x="11" y="11" width="9" height="9" fill="#ffb900"/>
                </svg>
                Přihlásit se přes Microsoft TUL
            </a>
            
            <a href="{mock_href}" class="btn btn-mock">
                🧪 Vývojářská simulace (Mock Login)
            </a>
            
            <div class="status-box">
                <div class="status-item">
                    <span>Discord Bot:</span>
                    <span class="status-value">{bot_status}</span>
                </div>
                <div class="status-item">
                    <span>Discord Server:</span>
                    <span class="status-value">{guild_name}</span>
                </div>
                <div class="status-item">
                    <span>Session:</span>
                    <span class="status-value">{state if state else "Přímo z webu"}</span>
                </div>
            </div>
        </div>
    </body>
    </html>
    """

@app.get("/login")
async def login(state: Optional[str] = None):
    """Zahajeni OAuth2 autorizace pres Microsoft Entra ID."""
    if not MICROSOFT_CLIENT_ID or MICROSOFT_CLIENT_ID == "SEM_VLOZTE_CLIENT_ID":
        return HTMLResponse(
            """
            <body style="font-family: sans-serif; background: #0f172a; color: #f8fafc; padding: 40px; text-align: center;">
                <h2>⚠️ Microsoft OAuth dosud není nakonfigurován</h2>
                <p style="color: #94a3b8; margin: 16px 0;">V souboru <code>.env</code> je potřeba nastavit <code>MICROSOFT_CLIENT_ID</code> a <code>MICROSOFT_CLIENT_SECRET</code> zadané od LIANE.</p>
                <p>Pro testování funkčnosti můžete využít <a href="/mock-login" style="color: #38bdf8;">Vývojářskou simulaci (Mock Login)</a>.</p>
            </body>
            """, status_code=500
        )
    
    # Pokud nebyl predan state z bota, vygenerujeme nahodny
    current_state = state or str(uuid.uuid4())
    if current_state not in verification_states:
        verification_states[current_state] = {"discord_id": None, "created_at": time.time()}
    
    # Parametry pro Microsoft OAuth2 v2.0
    params = {
        "client_id": MICROSOFT_CLIENT_ID,
        "response_type": "code",
        "redirect_uri": REDIRECT_URI,
        "response_mode": "query",
        "scope": "User.Read openid profile email",
        "state": current_state
    }
    
    auth_url = f"{MS_AUTHORIZE_URL}?" + "&".join(f"{k}={v}" for k, v in params.items())
    return RedirectResponse(auth_url)


@app.get("/callback")
async def callback(code: Optional[str] = None, state: Optional[str] = None, error: Optional[str] = None, error_description: Optional[str] = None):
    """Zpracovani navratoveho volani z Microsoft Identity platformy."""
    if error:
        return render_result_page(
            success=False,
            title="Přihlášení bylo zamítnuto",
            message=f"Chyba při přihlašování: {error_description or error}",
            details="Pokud nejste studentem či zaměstnancem FM, přístup do interních kanálů není povolen."
        )
    
    if not code:
        raise HTTPException(status_code=400, detail="Chybí autorizační kód (code).")
    
    # 1. Vymena autorizacniho kodu za Access Token
    token_data = {
        "client_id": MICROSOFT_CLIENT_ID,
        "client_secret": MICROSOFT_CLIENT_SECRET,
        "grant_type": "authorization_code",
        "code": code,
        "redirect_uri": REDIRECT_URI,
    }
    
    async with httpx.AsyncClient() as client:
        token_resp = await client.post(MS_TOKEN_URL, data=token_data)
        if token_resp.status_code != 200:
            return render_result_page(
                success=False,
                title="Chyba při získávání tokenu",
                message="Nepodařilo se ověřit autorizační kód vůči Microsoft TUL.",
                details=token_resp.text
            )
        tokens = token_resp.json()
        access_token = tokens.get("access_token")
        
        # 2. Ziskani profilu prihlaseneho uzivatele z Microsoft Graph API
        graph_resp = await client.get(
            MS_GRAPH_ME_URL,
            headers={"Authorization": f"Bearer {access_token}"}
        )
        if graph_resp.status_code != 200:
            return render_result_page(
                success=False,
                title="Chyba profilu",
                message="Nepodařilo se načíst data o uživateli z Microsoft Graph API.",
                details=graph_resp.text
            )
        profile = graph_resp.json()

    # 3. Zpracovani uzivatelskych dat
    display_name = profile.get("displayName", "Neznámý")
    email = profile.get("mail") or profile.get("userPrincipalName", "")
    job_title = profile.get("jobTitle")
    
    # Urceni zda jde o zamestnance ci studenta
    is_staff = bool(job_title) or ("zamestnanec" in email.lower())
    
    user_info = {
        "name": display_name,
        "email": email,
        "is_staff": is_staff,
        "job_title": job_title
    }
    
    # 4. Propojeni s Discord uctem (pokud prisel z overovaciho odkazu)
    discord_result = None
    session_data = verification_states.get(state or "")
    if session_data and session_data.get("discord_id"):
        discord_id = session_data["discord_id"]
        discord_result = await assign_discord_role(discord_id, user_info)
        # Smazani pouziteho tokenu
        verification_states.pop(state, None)
    
    return render_result_page(
        success=True,
        title="Ověření proběhlo úspěšně! 🎉",
        message=f"Vítej, **{display_name}** ({email}).",
        details="Tvoje univerzitní identita FM TUL byla úspěšně ověřena.",
        discord_result=discord_result
    )


@app.get("/mock-login")
async def mock_login(state: Optional[str] = None):
    """Simulovane prihlaseni pro lokalni vyvoj bez nutnosti pripojeni k TUL tenantu."""
    user_info = {
        "name": "Jakub Marcinka",
        "email": "jakub.marcinka@tul.cz",
        "is_staff": False,
        "job_title": None,
        "faculty": "FM"
    }
    
    discord_result = None
    session_data = verification_states.get(state or "")
    if session_data and session_data.get("discord_id"):
        discord_id = session_data["discord_id"]
        discord_result = await assign_discord_role(discord_id, user_info)
        verification_states.pop(state, None)
    
    return render_result_page(
        success=True,
        title="Vývojářské ověření (Mock) úspěšné 🧪",
        message=f"Simulováno přihlášení studenta: **{user_info['name']}** ({user_info['email']}).",
        details="Toto je simulovaný režim pro lokální testování.",
        discord_result=discord_result
    )


def render_result_page(success: bool, title: str, message: str, details: str = "", discord_result: Optional[dict] = None) -> HTMLResponse:
    """Pomocna funkce pro vykresleni vysledkove HTML stranky."""
    color = "#10b981" if success else "#ef4444"
    icon = "✅" if success else "❌"
    
    discord_box = ""
    if discord_result:
        if discord_result.get("success"):
            discord_box = f"""
            <div style="background: #0f172a; border-radius: 10px; padding: 16px; margin-top: 20px; border-left: 4px solid #10b981;">
                <p style="color: #f8fafc; font-weight: 600; margin-bottom: 4px;">Účet na Discordu spárován!</p>
                <p style="color: #94a3b8; font-size: 14px;">Byla vám přidělena role: <span style="color: #38bdf8;">{discord_result.get('role_name')}</span>.</p>
                <p style="color: #94a3b8; font-size: 13px; margin-top: 6px;">Můžete se vrátit zpět do aplikace Discord.</p>
            </div>
            """
        else:
            discord_box = f"""
            <div style="background: #0f172a; border-radius: 10px; padding: 16px; margin-top: 20px; border-left: 4px solid #eab308;">
                <p style="color: #f8fafc; font-weight: 600; margin-bottom: 4px;">Upozornění k Discord účtu:</p>
                <p style="color: #94a3b8; font-size: 14px;">{discord_result.get('message')}</p>
            </div>
            """
    
    html = f"""
    <!DOCTYPE html>
    <html lang="cs">
    <head>
        <meta charset="UTF-8">
        <meta name="viewport" content="width=device-width, initial-scale=1.0">
        <title>{title} - FM TUL</title>
        <link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700&display=swap" rel="stylesheet">
        <style>
            * {{ margin: 0; padding: 0; box-sizing: border-box; font-family: 'Inter', sans-serif; }}
            body {{ background: #0f172a; color: #f8fafc; display: flex; align-items: center; justify-content: center; min-height: 100vh; padding: 20px; }}
            .card {{ background: #1e293b; border: 1px solid #334155; border-radius: 16px; width: 100%; max-width: 520px; padding: 36px; box-shadow: 0 20px 25px -5px rgba(0,0,0,0.4); text-align: center; }}
            .icon {{ font-size: 48px; margin-bottom: 16px; }}
            h1 {{ font-size: 24px; font-weight: 700; margin-bottom: 12px; color: #f8fafc; }}
            p.msg {{ color: #cbd5e1; font-size: 16px; line-height: 1.5; margin-bottom: 16px; }}
            p.details {{ color: #94a3b8; font-size: 14px; margin-bottom: 24px; }}
            a.btn {{ display: inline-block; padding: 12px 24px; background: #2563eb; color: white; border-radius: 8px; text-decoration: none; font-weight: 600; font-size: 15px; }}
            a.btn:hover {{ background: #1d4ed8; }}
        </style>
    </head>
    <body>
        <div class="card">
            <div class="icon">{icon}</div>
            <h1>{title}</h1>
            <p class="msg">{message}</p>
            {f'<p class="details">{details}</p>' if details else ''}
            {discord_box}
            <div style="margin-top: 24px;">
                <a href="/" class="btn">Zpět na hlavní stránku</a>
            </div>
        </div>
    </body>
    </html>
    """
    return HTMLResponse(content=html, status_code=200 if success else 400)


# --- SPUSTENI SLUZEB ---

def run_discord_bot():
    """Spusteni Discord bota v samostatnem vlakne."""
    if DISCORD_TOKEN and DISCORD_TOKEN != "SEM_VLOZTE_DISCORD_BOT_TOKEN":
        try:
            bot.run(DISCORD_TOKEN)
        except Exception as e:
            print(f"❌ Chyba pri spusteni Discord bota: {e}")
    else:
        print("ℹ️ DISCORD_TOKEN neni nastaven v .env – bot neni spusten (web server bezi dal).")

if __name__ == "__main__":
    # Spustime bota v background vlakne
    threading.Thread(target=run_discord_bot, daemon=True).start()
    
    # Spustime FastAPI aplikaci
    import uvicorn
    print(f"🚀 Spoustim webovy server na http://{APP_HOST}:{APP_PORT}")
    uvicorn.run(app, host=APP_HOST, port=APP_PORT)
