package runner

// Common is the short list of verbs most programs need, with a note on
// when each helps, in Chinese and English. The GUI's dependency dialog
// puts it first; `cellar deps common` prints it.
var Common = []CommonVerb{
	{"vcrun2022", "VC++ 2015–2022 运行库，大多数新软件需要", "VC++ 2015–2022 runtime, needed by most newer software", false},
	{"vcrun2013", "VC++ 2013 运行库", "VC++ 2013 runtime", false},
	{"vcrun2010", "VC++ 2010 运行库", "VC++ 2010 runtime", false},
	{"vcrun2008", "VC++ 2008 运行库，较老的软件", "VC++ 2008 runtime for older software", false},
	{"vcrun2005", "VC++ 2005 运行库", "VC++ 2005 runtime", false},
	{"vcrun6", "VC6 运行库，很老的软件", "VC6 runtime for very old software", false},
	{"mfc42", "MFC 4.2，老式国产软件常用", "MFC 4.2, commonly used by older Chinese software", false},
	{"vb6run", "VB6 运行库，老式国产软件常用", "VB6 runtime, commonly used by older Chinese software", false},
	{"riched20", "RichEdit 控件，聊天框、编辑框显示异常时装", "RichEdit control, useful when chat or edit boxes render incorrectly", false},
	{"gdiplus", "GDI+，图片或图表显示异常时装", "GDI+, useful when images or charts render incorrectly", false},
	{"msxml6", "MSXML 6", "MSXML 6", false},
	{"msxml3", "MSXML 3，老软件读写 XML 用", "MSXML 3, used by older software to read and write XML", false},
	{"quartz", "DirectShow，程序里放视频", "DirectShow, for programs that play video", false},
	{"corefonts", "微软核心英文字体（Arial、Times 等）", "Microsoft core fonts (Arial, Times, and others)", false},
	{"d3dx9", "DirectX 9 组件，老游戏", "DirectX 9 components for older games", false},
	{"d3dcompiler_47", "D3D 着色器编译器，新一些的程序和浏览器内核用", "D3D shader compiler, used by newer programs and browser engines", false},
	{"xact", "XAudio / XACT 音频，游戏没声音时装", "XAudio / XACT audio, useful when games have no sound", false},
	{"dxvk", "DXVK：用 Vulkan 跑 D3D 9–11，游戏用", "DXVK: runs D3D 9–11 through Vulkan for games", false},
	{"vkd3d", "VKD3D：用 Vulkan 跑 D3D 12", "VKD3D: runs D3D 12 through Vulkan", false},
	{"dotnet48", ".NET Framework 4.8", ".NET Framework 4.8", true},
	{"dotnet40", ".NET Framework 4.0", ".NET Framework 4.0", true},
	{"dotnet35", ".NET Framework 3.5", ".NET Framework 3.5", true},
	{"dotnetdesktop8", ".NET 8 桌面运行时", ".NET 8 desktop runtime", false},
}

// CommonVerb is one entry of Common.
type CommonVerb struct {
	Verb string
	Zh   string
	En   string
	Slow bool // takes many minutes (.NET)
}
