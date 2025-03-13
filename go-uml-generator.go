package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// UMLGenerator verwaltet die UML-Diagramm-Generierung
type UMLGenerator struct {
	structs    map[string]*StructInfo
	interfaces map[string]*InterfaceInfo
	relations  []Relation
}

// StructInfo enthält Informationen über eine Struct
type StructInfo struct {
	Name    string
	Fields  []FieldInfo
	Methods []MethodInfo
}

// InterfaceInfo enthält Informationen über ein Interface
type InterfaceInfo struct {
	Name    string
	Methods []MethodInfo
}

// FieldInfo repräsentiert ein Feld in einer Struct
type FieldInfo struct {
	Name string
	Type string
}

// MethodInfo repräsentiert eine Methode
type MethodInfo struct {
	Name       string
	Parameters []ParameterInfo
	ReturnType string
}

// ParameterInfo repräsentiert einen Parameter einer Methode
type ParameterInfo struct {
	Name string
	Type string
}

// Relation repräsentiert eine Beziehung zwischen Typen
type Relation struct {
	From        string
	To          string
	Type        string // "extends", "implements", "aggregation", "composition"
	Cardinality string
}

// FileWatcher überwacht Dateiänderungen in einem Verzeichnis
type FileWatcher struct {
	dirPath      string               // Pfad zum zu überwachenden Verzeichnis
	lastModified map[string]time.Time // Speichert letzte Änderungszeit pro Datei
	outputDir    string
}

func NewUMLGenerator() *UMLGenerator {
	return &UMLGenerator{
		structs:    make(map[string]*StructInfo),
		interfaces: make(map[string]*InterfaceInfo),
		relations:  []Relation{},
	}
}

func (g *UMLGenerator) Reset() {
	g.structs = make(map[string]*StructInfo)
	g.interfaces = make(map[string]*InterfaceInfo)
	g.relations = []Relation{}
}

func (g *UMLGenerator) ParseGoFile(filePath string) error {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("Fehler beim Parsen der Datei %s: %v", filePath, err)
	}

	// Durchlaufe alle Deklarationen im AST
	for _, decl := range node.Decls {
		// Typ-Deklarationen verarbeiten
		if genDecl, ok := decl.(*ast.GenDecl); ok && genDecl.Tok == token.TYPE {
			for _, spec := range genDecl.Specs {
				if typeSpec, ok := spec.(*ast.TypeSpec); ok {
					g.processTypeSpec(typeSpec)
				}
			}
		}

		// Methoden verarbeiten
		if funcDecl, ok := decl.(*ast.FuncDecl); ok && funcDecl.Recv != nil {
			g.processMethod(funcDecl)
		}
	}

	// Beziehungen identifizieren
	g.identifyRelations()

	return nil
}

func (g *UMLGenerator) processTypeSpec(typeSpec *ast.TypeSpec) {
	typeName := typeSpec.Name.Name

	// Struct verarbeiten
	if structType, ok := typeSpec.Type.(*ast.StructType); ok {
		structInfo := &StructInfo{Name: typeName, Fields: []FieldInfo{}, Methods: []MethodInfo{}}

		// Felder extrahieren
		if structType.Fields != nil {
			for _, field := range structType.Fields.List {
				fieldType := getTypeString(field.Type)

				if len(field.Names) > 0 {
					for _, name := range field.Names {
						structInfo.Fields = append(structInfo.Fields, FieldInfo{
							Name: name.Name,
							Type: fieldType,
						})
					}
				} else {
					// Anonymes Feld (Embedding)
					structInfo.Fields = append(structInfo.Fields, FieldInfo{
						Name: fieldType,
						Type: fieldType,
					})
				}
			}
		}

		g.structs[typeName] = structInfo
		return
	}

	// Interface verarbeiten
	if interfaceType, ok := typeSpec.Type.(*ast.InterfaceType); ok {
		interfaceInfo := &InterfaceInfo{Name: typeName, Methods: []MethodInfo{}}

		// Interface-Methoden extrahieren
		if interfaceType.Methods != nil {
			for _, method := range interfaceType.Methods.List {
				if len(method.Names) > 0 {
					methodName := method.Names[0].Name

					// Methoden-Parameter und Rückgabewerte
					if funcType, ok := method.Type.(*ast.FuncType); ok {
						methodInfo := MethodInfo{Name: methodName, Parameters: []ParameterInfo{}}

						// Parameter
						if funcType.Params != nil {
							for _, param := range funcType.Params.List {
								paramType := getTypeString(param.Type)

								if len(param.Names) > 0 {
									for _, name := range param.Names {
										methodInfo.Parameters = append(methodInfo.Parameters, ParameterInfo{
											Name: name.Name,
											Type: paramType,
										})
									}
								} else {
									methodInfo.Parameters = append(methodInfo.Parameters, ParameterInfo{
										Name: "",
										Type: paramType,
									})
								}
							}
						}

						// Rückgabewerte
						if funcType.Results != nil {
							var returnTypes []string
							for _, result := range funcType.Results.List {
								returnType := getTypeString(result.Type)
								returnTypes = append(returnTypes, returnType)
							}
							methodInfo.ReturnType = strings.Join(returnTypes, ", ")
						}

						interfaceInfo.Methods = append(interfaceInfo.Methods, methodInfo)
					}
				}
			}
		}

		g.interfaces[typeName] = interfaceInfo
		return
	}
}

func (g *UMLGenerator) processMethod(funcDecl *ast.FuncDecl) {
	// Receiver-Typ ermitteln
	if funcDecl.Recv == nil || len(funcDecl.Recv.List) == 0 {
		return // Keine Receiver, also keine Methode
	}

	receiver := funcDecl.Recv.List[0]
	var typeName string

	// Pointer-Receiver oder Wert-Receiver
	switch typeExpr := receiver.Type.(type) {
	case *ast.StarExpr:
		if ident, ok := typeExpr.X.(*ast.Ident); ok {
			typeName = ident.Name
		}
	case *ast.Ident:
		typeName = typeExpr.Name
	default:
		return
	}

	// Methoden-Info erstellen
	methodName := funcDecl.Name.Name
	methodInfo := MethodInfo{Name: methodName, Parameters: []ParameterInfo{}}

	// Parameter
	if funcDecl.Type.Params != nil {
		for _, param := range funcDecl.Type.Params.List {
			paramType := getTypeString(param.Type)

			if len(param.Names) > 0 {
				for _, name := range param.Names {
					methodInfo.Parameters = append(methodInfo.Parameters, ParameterInfo{
						Name: name.Name,
						Type: paramType,
					})
				}
			} else {
				methodInfo.Parameters = append(methodInfo.Parameters, ParameterInfo{
					Name: "",
					Type: paramType,
				})
			}
		}
	}

	// Rückgabewerte
	if funcDecl.Type.Results != nil {
		var returnTypes []string
		for _, result := range funcDecl.Type.Results.List {
			returnType := getTypeString(result.Type)
			returnTypes = append(returnTypes, returnType)
		}
		methodInfo.ReturnType = strings.Join(returnTypes, ", ")
	}

	// Methode zur entsprechenden Struct hinzufügen
	if structInfo, ok := g.structs[typeName]; ok {
		structInfo.Methods = append(structInfo.Methods, methodInfo)
	}
}

func (g *UMLGenerator) identifyRelations() {
	// Embedding und Komposition identifizieren
	for structName, structInfo := range g.structs {
		for _, field := range structInfo.Fields {
			// Prüfe, ob der Feldtyp eine bekannte Struct ist
			if _, ok := g.structs[field.Type]; ok {
				relationType := "aggregation"
				if field.Name == field.Type {
					// Embedding: Feld hat den gleichen Namen wie der Typ
					relationType = "extends"
				} else if strings.HasPrefix(field.Type, "*") {
					// Pointer könnte Komposition sein
					relationType = "composition"
				}

				g.relations = append(g.relations, Relation{
					From:        structName,
					To:          strings.TrimPrefix(field.Type, "*"),
					Type:        relationType,
					Cardinality: "1",
				})
			}

			// Prüfe, ob der Feldtyp ein Interface ist
			if _, ok := g.interfaces[field.Type]; ok {
				g.relations = append(g.relations, Relation{
					From:        structName,
					To:          field.Type,
					Type:        "implements",
					Cardinality: "",
				})
			}
		}
	}

	// Interfaces und Implementierungen prüfen
	for structName, structInfo := range g.structs {
		for interfaceName, interfaceInfo := range g.interfaces {
			// Prüfe, ob die Struct das Interface implementiert
			implementsInterface := true
			for _, interfaceMethod := range interfaceInfo.Methods {
				found := false
				for _, structMethod := range structInfo.Methods {
					if structMethod.Name == interfaceMethod.Name {
						found = true
						break
					}
				}
				if !found {
					implementsInterface = false
					break
				}
			}

			if implementsInterface && len(interfaceInfo.Methods) > 0 {
				g.relations = append(g.relations, Relation{
					From:        structName,
					To:          interfaceName,
					Type:        "implements",
					Cardinality: "",
				})
			}
		}
	}
}

// getTypeString konvertiert einen AST-Typ in eine String-Repräsentation
func getTypeString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + getTypeString(t.X)
	case *ast.SelectorExpr:
		return getTypeString(t.X) + "." + t.Sel.Name
	case *ast.ArrayType:
		if t.Len == nil {
			return "[]" + getTypeString(t.Elt)
		}
		return "[n]" + getTypeString(t.Elt)
	case *ast.MapType:
		return "map[" + getTypeString(t.Key) + "]" + getTypeString(t.Value)
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.ChanType:
		switch t.Dir {
		case ast.SEND:
			return "chan<- " + getTypeString(t.Value)
		case ast.RECV:
			return "<-chan " + getTypeString(t.Value)
		default:
			return "chan " + getTypeString(t.Value)
		}
	case *ast.FuncType:
		return "func"
	case *ast.StructType:
		return "struct"
	case *ast.Ellipsis:
		return "..." + getTypeString(t.Elt)
	default:
		return "unknown"
	}
}

// Findet rekursiv alle Go-Dateien in einem Verzeichnis
func findGoFiles(dirPath string) ([]string, error) {
	var files []string

	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() && strings.HasSuffix(info.Name(), ".go") {
			files = append(files, path)
		}

		return nil
	})

	return files, err
}

// GenerateUMLFromDirectory parst alle Go-Dateien in einem Verzeichnis
func (g *UMLGenerator) GenerateUMLFromDirectory(dirPath string) error {
	g.Reset()

	// Alle Go-Dateien im Verzeichnis finden
	goFiles, err := findGoFiles(dirPath)
	if err != nil {
		return fmt.Errorf("Fehler beim Durchsuchen des Verzeichnisses: %v", err)
	}

	fmt.Printf("Gefundene Go-Dateien: %d\n", len(goFiles))

	// Jede Go-Datei parsen
	for _, filePath := range goFiles {
		fmt.Printf("Verarbeite: %s\n", filePath)
		if err := g.ParseGoFile(filePath); err != nil {
			return err
		}
	}

	return nil
}

// UML-Diagramm als PlantUML generieren
func (g *UMLGenerator) GeneratePlantUML() string {
	var sb strings.Builder

	sb.WriteString("@startuml\n\n")

	// Structs darstellen
	for _, structInfo := range g.structs {
		sb.WriteString(fmt.Sprintf("class %s {\n", structInfo.Name))

		// Felder
		for _, field := range structInfo.Fields {
			// Anonyme Felder (Embedding) nicht anzeigen
			if field.Name != field.Type {
				sb.WriteString(fmt.Sprintf("    +%s: %s\n", field.Name, field.Type))
			}
		}

		// Methoden
		for _, method := range structInfo.Methods {
			var params []string
			for _, param := range method.Parameters {
				if param.Name != "" {
					params = append(params, fmt.Sprintf("%s: %s", param.Name, param.Type))
				} else {
					params = append(params, param.Type)
				}
			}

			if method.ReturnType != "" {
				sb.WriteString(fmt.Sprintf("    +%s(%s): %s\n", method.Name, strings.Join(params, ", "), method.ReturnType))
			} else {
				sb.WriteString(fmt.Sprintf("    +%s(%s)\n", method.Name, strings.Join(params, ", ")))
			}
		}

		sb.WriteString("}\n\n")
	}

	// Interfaces darstellen
	for _, interfaceInfo := range g.interfaces {
		sb.WriteString(fmt.Sprintf("interface %s {\n", interfaceInfo.Name))

		// Interface-Methoden
		for _, method := range interfaceInfo.Methods {
			var params []string
			for _, param := range method.Parameters {
				if param.Name != "" {
					params = append(params, fmt.Sprintf("%s: %s", param.Name, param.Type))
				} else {
					params = append(params, param.Type)
				}
			}

			if method.ReturnType != "" {
				sb.WriteString(fmt.Sprintf("    +%s(%s): %s\n", method.Name, strings.Join(params, ", "), method.ReturnType))
			} else {
				sb.WriteString(fmt.Sprintf("    +%s(%s)\n", method.Name, strings.Join(params, ", ")))
			}
		}

		sb.WriteString("}\n\n")
	}

	// Beziehungen darstellen
	for _, relation := range g.relations {
		switch relation.Type {
		case "extends":
			sb.WriteString(fmt.Sprintf("%s <|-- %s\n", relation.To, relation.From))
		case "implements":
			sb.WriteString(fmt.Sprintf("%s <|.. %s\n", relation.To, relation.From))
		case "aggregation":
			sb.WriteString(fmt.Sprintf("%s o-- %s\n", relation.From, relation.To))
		case "composition":
			sb.WriteString(fmt.Sprintf("%s *-- %s\n", relation.From, relation.To))
		}
	}

	sb.WriteString("\n@enduml")
	return sb.String()
}

// Generiere UML-Diagramm als PNG
// Generiere UML-Diagramm als PNG mit HTTP POST-Anfrage
// Generiere UML-Diagramm mit lokaler PlantUML.jar
func (g *UMLGenerator) GenerateUMLDiagram(outputDir, fileName string) error {
	plantUML := g.GeneratePlantUML()

	// Stellen Sie sicher, dass das Ausgabeverzeichnis existiert
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("Fehler beim Erstellen des Ausgabeverzeichnisses: %v", err)
	}

	// PlantUML-Datei speichern
	plantUMLFilePath := filepath.Join(outputDir, fileName+".puml")
	if err := os.WriteFile(plantUMLFilePath, []byte(plantUML), 0644); err != nil {
		return fmt.Errorf("Fehler beim Speichern der PlantUML-Datei: %v", err)
	}

	fmt.Printf("PlantUML-Datei erstellt: %s\n", plantUMLFilePath)

	// Überprüfen, ob plantuml.jar verfügbar ist
	_, err := os.Stat("plantuml.jar")
	if os.IsNotExist(err) {
		fmt.Println("Hinweis: plantuml.jar nicht gefunden. Nur .puml-Datei wurde erstellt.")
		fmt.Println("Um ein PNG-Bild zu erzeugen, führen Sie folgenden Befehl aus:")
		fmt.Printf("java -jar plantuml.jar %s\n", plantUMLFilePath)
		return nil
	}

	// PNG mit lokaler plantuml.jar generieren
	cmd := exec.Command("java", "-jar", "plantuml.jar", plantUMLFilePath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Fehler beim Ausführen von PlantUML: %v\nAusgabe: %s", err, string(output))
	}

	pngFilePath := filepath.Join(outputDir, fileName+".png")
	fmt.Printf("UML-Diagramm erstellt: %s\n", pngFilePath)
	return nil
}

// Neue FileWatcher-Implementierung für Verzeichnisse
func NewFileWatcher(dirPath string, outputDir string) *FileWatcher {
	return &FileWatcher{
		dirPath:      dirPath,
		lastModified: make(map[string]time.Time),
		outputDir:    outputDir,
	}
}

func (w *FileWatcher) Watch() {
	// Initialisierung der letzten Änderungszeiten
	goFiles, err := findGoFiles(w.dirPath)
	if err != nil {
		fmt.Printf("Fehler beim Durchsuchen des Verzeichnisses: %v\n", err)
		return
	}

	for _, filePath := range goFiles {
		fileInfo, err := os.Stat(filePath)
		if err == nil {
			w.lastModified[filePath] = fileInfo.ModTime()
		}
	}

	// UML-Diagramm initial erstellen
	g := NewUMLGenerator()
	err = g.GenerateUMLFromDirectory(w.dirPath)
	if err != nil {
		fmt.Printf("Fehler beim Generieren des UML-Diagramms: %v\n", err)
		return
	}

	err = g.GenerateUMLDiagram(w.outputDir, "uml_diagram")
	if err != nil {
		fmt.Printf("Fehler beim Erstellen des UML-Diagramms: %v\n", err)
	}

	// Dateiänderungen überwachen
	for {
		time.Sleep(2 * time.Second)

		goFiles, err := findGoFiles(w.dirPath)
		if err != nil {
			fmt.Printf("Fehler beim Durchsuchen des Verzeichnisses: %v\n", err)
			continue
		}

		changed := false

		// Prüfen, ob sich Dateien geändert haben oder neue hinzugekommen sind
		for _, filePath := range goFiles {
			fileInfo, err := os.Stat(filePath)
			if err != nil {
				continue
			}

			lastMod, exists := w.lastModified[filePath]
			if !exists || fileInfo.ModTime().After(lastMod) {
				w.lastModified[filePath] = fileInfo.ModTime()
				changed = true
			}
		}

		// Prüfen, ob Dateien gelöscht wurden
		for filePath := range w.lastModified {
			exists := false
			for _, goFile := range goFiles {
				if goFile == filePath {
					exists = true
					break
				}
			}

			if !exists {
				delete(w.lastModified, filePath)
				changed = true
			}
		}

		// Bei Änderungen UML-Diagramm neu generieren
		if changed {
			fmt.Println("Änderungen erkannt, UML-Diagramm wird aktualisiert...")

			g := NewUMLGenerator()
			err = g.GenerateUMLFromDirectory(w.dirPath)
			if err != nil {
				fmt.Printf("Fehler beim Generieren des UML-Diagramms: %v\n", err)
				continue
			}

			err = g.GenerateUMLDiagram(w.outputDir, "uml_diagram")
			if err != nil {
				fmt.Printf("Fehler beim Erstellen des UML-Diagramms: %v\n", err)
			}
		}
	}
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Verwendung: uml-watcher <Verzeichnispfad> [Ausgabeverzeichnis]")
		return
	}

	dirPath := os.Args[1]
	outputDir := "output"

	if len(os.Args) > 2 {
		outputDir = os.Args[2]
	}

	watcher := NewFileWatcher(dirPath, outputDir)
	watcher.Watch()
}
