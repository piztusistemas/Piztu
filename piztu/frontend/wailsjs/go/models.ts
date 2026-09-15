export namespace actualizacion {
	
	export class Estado {
	    disponible: boolean;
	    version_local: string;
	    version_remota: string;
	    url_descarga: string;
	    url_checksum: string;
	    notas: string;
	
	    static createFrom(source: any = {}) {
	        return new Estado(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.disponible = source["disponible"];
	        this.version_local = source["version_local"];
	        this.version_remota = source["version_remota"];
	        this.url_descarga = source["url_descarga"];
	        this.url_checksum = source["url_checksum"];
	        this.notas = source["notas"];
	    }
	}

}

export namespace main {
	
	export class AccionInfo {
	    id: string;
	    modulo: string;
	    etiqueta: string;
	    icona: string;
	
	    static createFrom(source: any = {}) {
	        return new AccionInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.modulo = source["modulo"];
	        this.etiqueta = source["etiqueta"];
	        this.icona = source["icona"];
	    }
	}
	export class AulaConfig {
	    prefixo: string;
	    dominio: string;
	    nequipos: number;
	    sshuser: string;
	    saltmaster: string;
	
	    static createFrom(source: any = {}) {
	        return new AulaConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.prefixo = source["prefixo"];
	        this.dominio = source["dominio"];
	        this.nequipos = source["nequipos"];
	        this.sshuser = source["sshuser"];
	        this.saltmaster = source["saltmaster"];
	    }
	}
	export class Equipo {
	    nome: string;
	    x: number;
	    y: number;
	
	    static createFrom(source: any = {}) {
	        return new Equipo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.nome = source["nome"];
	        this.x = source["x"];
	        this.y = source["y"];
	    }
	}
	export class HostCredencial {
	    ip: string;
	    usuario: string;
	    contrasinal: string;
	
	    static createFrom(source: any = {}) {
	        return new HostCredencial(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ip = source["ip"];
	        this.usuario = source["usuario"];
	        this.contrasinal = source["contrasinal"];
	    }
	}
	export class IdiomaInfo {
	    code: string;
	    nome: string;
	
	    static createFrom(source: any = {}) {
	        return new IdiomaInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.nome = source["nome"];
	    }
	}
	export class ModuloInfo {
	    id: string;
	    clave: string;
	    nome: string;
	    icona: string;
	    iconaImaxeDataURI: string;
	    activo: boolean;
	    interno: boolean;
	    dispo: boolean;
	    motivo: string;
	    instalado: boolean;
	    novo: boolean;
	    ten_panel: boolean;
	    ruta: string;
	    descargable: boolean;
	    version: string;
	    version_nova: string;
	    actualizable: boolean;
	    autor: string;
	    email: string;
	    web: string;
	    mantenPiztuAberto: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ModuloInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.clave = source["clave"];
	        this.nome = source["nome"];
	        this.icona = source["icona"];
	        this.iconaImaxeDataURI = source["iconaImaxeDataURI"];
	        this.activo = source["activo"];
	        this.interno = source["interno"];
	        this.dispo = source["dispo"];
	        this.motivo = source["motivo"];
	        this.instalado = source["instalado"];
	        this.novo = source["novo"];
	        this.ten_panel = source["ten_panel"];
	        this.ruta = source["ruta"];
	        this.descargable = source["descargable"];
	        this.version = source["version"];
	        this.version_nova = source["version_nova"];
	        this.actualizable = source["actualizable"];
	        this.autor = source["autor"];
	        this.email = source["email"];
	        this.web = source["web"];
	        this.mantenPiztuAberto = source["mantenPiztuAberto"];
	    }
	}
	export class MotorInfo {
	    nome: string;
	    etiqueta: string;
	    dispo: boolean;
	    motivo: string;
	    recomendar: boolean;
	
	    static createFrom(source: any = {}) {
	        return new MotorInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.nome = source["nome"];
	        this.etiqueta = source["etiqueta"];
	        this.dispo = source["dispo"];
	        this.motivo = source["motivo"];
	        this.recomendar = source["recomendar"];
	    }
	}
	export class PanelResposta {
	    panel?: modulos.Panel;
	    valores: Record<string, string>;
	    equipos: string[];
	
	    static createFrom(source: any = {}) {
	        return new PanelResposta(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.panel = this.convertValues(source["panel"], modulos.Panel);
	        this.valores = source["valores"];
	        this.equipos = source["equipos"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace modulos {
	
	export class Campo {
	    tipo: string;
	    id: string;
	    etiqueta: string;
	    axuda: string;
	    defecto: string;
	    opcions: string[];
	    fonte: string;
	    accion: string;
	
	    static createFrom(source: any = {}) {
	        return new Campo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tipo = source["tipo"];
	        this.id = source["id"];
	        this.etiqueta = source["etiqueta"];
	        this.axuda = source["axuda"];
	        this.defecto = source["defecto"];
	        this.opcions = source["opcions"];
	        this.fonte = source["fonte"];
	        this.accion = source["accion"];
	    }
	}
	export class Panel {
	    titulo: string;
	    campos: Campo[];
	
	    static createFrom(source: any = {}) {
	        return new Panel(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.titulo = source["titulo"];
	        this.campos = this.convertValues(source["campos"], Campo);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

