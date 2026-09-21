import os
from onelogin.saml2.auth import OneLogin_Saml2_Auth
from onelogin.saml2.settings import OneLogin_Saml2_Settings
import subprocess

def generate_keys():
    print("Generuji RSA klice pro SP...")
    saml_dir = os.path.join(os.path.dirname(__file__), "saml")
    if not os.path.exists(saml_dir):
        os.makedirs(saml_dir)
    
    cert_path = os.path.join(saml_dir, "sp.crt")
    key_path = os.path.join(saml_dir, "sp.key")
    
    if os.path.exists(cert_path) and os.path.exists(key_path):
        print("Klice jiz existuji, preskakuji generovani.")
        return
        
    # Pouzijeme openssl pro generovani samopodepsaneho certifikatu
    cmd = [
        "openssl", "req", "-new", "-newkey", "rsa:2048", "-days", "3650", "-nodes", "-x509",
        "-subj", "/CN=TUL-Discord-Bridge",
        "-keyout", key_path,
        "-out", cert_path
    ]
    subprocess.run(cmd, check=True)
    print(f"Klice ulozeny do {saml_dir}")

def generate_metadata():
    print("Generuji XML metadata pro registraci u LIANE...")
    saml_dir = os.path.join(os.path.dirname(__file__), "saml")
    settings_path = os.path.join(saml_dir, "settings.json")
    
    with open(settings_path, 'r') as f:
        settings_data = json.load(f)
        
    # Nacteme vygenerovany certifikat do settings
    with open(os.path.join(saml_dir, "sp.crt"), 'r') as f:
        cert_data = f.read().replace("-----BEGIN CERTIFICATE-----", "").replace("-----END CERTIFICATE-----", "").replace("\n", "")
        settings_data["sp"]["x509cert"] = cert_data
        
    # Ulozime zpet s certifikatem
    with open(settings_path, 'w') as f:
        json.dump(settings_data, f, indent=4)

    settings = OneLogin_Saml2_Settings(settings_data, custom_base_path=saml_dir)
    metadata = settings.get_sp_metadata()
    errors = settings.validate_metadata(metadata)

    if len(errors) == 0:
        metadata_path = os.path.join(saml_dir, "metadata.xml")
        with open(metadata_path, 'w') as f:
            if isinstance(metadata, bytes):
                f.write(metadata.decode('utf-8'))
            else:
                f.write(metadata)
        print(f"Metadata uspesne vygenerovana a ulozena do {metadata_path}")
        print("\n--- POSLI TENTO SOUBOR SPRAVCUM LIANE ---")
    else:
        print(f"Chyba pri generovani metadat: {', '.join(errors)}")

if __name__ == "__main__":
    import json
    generate_keys()
    generate_metadata()
