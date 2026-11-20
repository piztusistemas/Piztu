package motor

import "testing"

func TestParsearSOGrains(t *testing.T) {
	casos := []struct {
		nome   string
		stdout string
		host   string
		quere  SO
	}{
		{
			nome:   "windows",
			stdout: `{"bac01": {"kernel": "Windows"}}`,
			host:   "bac01",
			quere:  SOWindows,
		},
		{
			nome:   "linux debian",
			stdout: `{"aula07": {"kernel": "Linux"}}`,
			host:   "aula07",
			quere:  SOLinux,
		},
		{
			nome:   "maiúsculas/espazos",
			stdout: `{"pc1": {"kernel": "  linux  "}}`,
			host:   "pc1",
			quere:  SOLinux,
		},
		{
			nome:   "host non presente na resposta",
			stdout: `{"outro": {"kernel": "Linux"}}`,
			host:   "bac01",
			quere:  SODescoñecido,
		},
		{
			nome:   "minion sen resposta (salt devolve texto, non JSON)",
			stdout: "bac01:\n    Minion did not return",
			host:   "bac01",
			quere:  SODescoñecido,
		},
		{
			nome:   "kernel baleiro (WMI roto) pero shell delata Windows",
			stdout: `{"bac01": {"kernel": "", "os_family": "", "shell": "C:\\WINDOWS\\system32\\cmd.exe"}}`,
			host:   "bac01",
			quere:  SOWindows,
		},
		{
			nome:   "kernel baleiro pero os_family=Windows",
			stdout: `{"bac01": {"kernel": "", "os_family": "Windows", "shell": ""}}`,
			host:   "bac01",
			quere:  SOWindows,
		},
		{
			nome:   "kernel baleiro, shell POSIX → Linux",
			stdout: `{"aula07": {"kernel": "", "os_family": "", "shell": "/bin/sh"}}`,
			host:   "aula07",
			quere:  SOLinux,
		},
		{
			nome:   "todo baleiro",
			stdout: `{"bac01": {"kernel": "", "os_family": "", "shell": ""}}`,
			host:   "bac01",
			quere:  SODescoñecido,
		},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			if got := parsearSOGrains(c.stdout, c.host); got != c.quere {
				t.Fatalf("parsearSOGrains(%q) = %q, quería %q", c.stdout, got, c.quere)
			}
		})
	}
}

func TestPistaSOInventario(t *testing.T) {
	casos := []struct {
		nome  string
		vars  string
		quere SO
	}{
		{"sen vars", "", SODescoñecido},
		{"só mac", "mac=aa:bb:cc:dd:ee:ff", SODescoñecido},
		{"shell_type powershell", "mac=aa:bb:cc:dd:ee:ff ansible_shell_type=powershell", SOWindows},
		{"connection winrm", "ansible_connection=winrm", SOWindows},
		{"connection psrp", "ansible_connection=psrp", SOWindows},
		{"connection ssh (non decide)", "ansible_connection=ssh", SODescoñecido},
		{"override so=windows", "so=windows", SOWindows},
		{"override piztu_so=linux", "piztu_so=linux ansible_shell_type=powershell", SOLinux},
		{"override so=mac", "so=mac", SOMac},
		{"maiúsculas", "ANSIBLE_SHELL_TYPE=PowerShell", SOWindows},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			if got := pistaSOInventario(c.vars); got != c.quere {
				t.Fatalf("pistaSOInventario(%q) = %q, quería %q", c.vars, got, c.quere)
			}
		})
	}
}

func TestParsearSondaSO(t *testing.T) {
	casos := map[string]SO{
		"Linux\n":  SOLinux,
		"Darwin\n": SOMac,
		"'uname' no se reconoce como un comando interno o externo,\r\n\r\nMicrosoft Windows [Versión 10.0.22631.4460]\r\n": SOWindows,
		// Windows con PowerShell coma shell de OpenSSH (bac01): `uname` falla e `cmd /c ver` responde.
		"uname : El término 'uname' no se reconoce como nombre de un cmdlet\r\n    + CategoryInfo          : ObjectNotFound: (uname:String) [], CommandNotFoundException\r\n\nMicrosoft Windows [Versión 10.0.19045.5131]\r\n": SOWindows,
		// Só o erro de PowerShell, sen `ver`.
		"    + CategoryInfo          : ObjectNotFound: (uname:String) [], CommandNotFoundException\r\n    + FullyQualifiedErrorId : CommandNotFoundException\r\n": SOWindows,
		"": SODescoñecido,
	}
	for saida, quere := range casos {
		if so := parsearSondaSO(saida); so != quere {
			t.Errorf("parsearSondaSO(%q) = %q, quería %q", saida, so, quere)
		}
	}
}
