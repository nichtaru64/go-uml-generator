package main

import (
        "fmt"
        "go/ast"
        "go/parser"
        "go/token"
        "io"
        "net/http"
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

// FileWatcher überwacht Dateiänderungen
type FileWatcher struct {
        filePath     string
        lastModified time.Time
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
                                        // Embedding
                                        relationType = "composition"
                                }
                                
                                g.relations = append(g.relations, Relation{
                                        From:        structName,
                                        To:          field.Type,
                                        Type:        relationType,
                                        Cardinality: "1",
                                })
                        }
                        
                        // Prüfe auf Slice/Array/Map von bekannten Typen
                        if strings.HasPrefix(field.Type, "[]") {
                                baseType := strings.TrimPrefix(field.Type, "[]")
                                if _, ok := g.structs[baseType]; ok {
                                        g.relations = append(g.relations, Relation{
                                                From:        structName,
                                                To:          baseType,
                                                Type:        "aggregation",
                                                Cardinality: "*",
                                        })
                                }
                        }
                }
        }
        
        // Interface-Implementierung prüfen
        // (Ein vereinfachter Ansatz, der nicht alle Fälle abdecken kann)
        for structName, structInfo := range g.structs {
                for interfaceName, interfaceInfo := range g.interfaces {
                        // Prüfe, ob die Struct alle Methoden des Interfaces hat
                        allMethodsImplemented := true
                        for _, interfaceMethod := range interfaceInfo.Methods {
                                methodFound := false
                                for _, structMethod := range structInfo.Methods {
                                        if interfaceMethod.Name == structMethod.Name {
                                                methodFound = true
                                                break
                                        }
                                }
                                if !methodFound {
                                        allMethodsImplemented = false
                                        break
                                }
                        }
                        
                        if allMethodsImplemented && len(interfaceInfo.Methods) > 0 {
                                g.relations = append(g.relations, Relation{
                                        From: structName,
                                        To:   interfaceName,
                                        Type: "implements",
                                })
                        }
                }
        }
}

func (g *UMLGenerator) GeneratePlantUML() string {
        var builder strings.Builder
        
        builder.WriteString("@startuml\n\n")
        
        // Klassen (Structs)
        for _, structInfo := range g.structs {
                builder.WriteString(fmt.Sprintf("class %s {\n", structInfo.Name))
                
                // Felder
                for _, field := range structInfo.Fields {
                        if field.Name == field.Type {
                                continue // Embedded-Typ, wird durch Beziehung dargestellt
                        }
                        builder.WriteString(fmt.Sprintf("  +%s: %s\n", field.Name, field.Type))
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
                        
                        returnStr := ""
                        if method.ReturnType != "" {
                                returnStr = fmt.Sprintf(": %s", method.ReturnType)
                        }
                        
                        builder.WriteString(fmt.Sprintf("  +%s(%s)%s\n", method.Name, strings.Join(params, ", "), returnStr))
                }
                
                builder.WriteString("}\n\n")
        }
        
        // Interfaces
        for _, interfaceInfo := range g.interfaces {
                builder.WriteString(fmt.Sprintf("interface %s {\n", interfaceInfo.Name))
                
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
                        
                        returnStr := ""
                        if method.ReturnType != "" {
                                returnStr = fmt.Sprintf(": %s", method.ReturnType)
                        }
                        
                        builder.WriteString(fmt.Sprintf("  +%s(%s)%s\n", method.Name, strings.Join(params, ", "), returnStr))
                }
                
                builder.WriteString("}\n\n")
        }
        
        // Beziehungen
        for _, relation := range g.relations {
                switch relation.Type {
                case "composition":
                        builder.WriteString(fmt.Sprintf("%s *-- %s\n", relation.From, relation.To))
                case "aggregation":
                        if relation.Cardinality == "*" {
                                builder.WriteString(fmt.Sprintf("%s o-- \"%s\" %s\n", relation.From, relation.Cardinality, relation.To))
                        } else {
                                builder.WriteString(fmt.Sprintf("%s o-- %s\n", relation.From, relation.To))
                        }
                case "implements":
                        builder.WriteString(fmt.Sprintf("%s ..|> %s\n", relation.From, relation.To))
                }
        }
        
        builder.WriteString("\n@enduml")
        return builder.String()
}

// Hilfsfunktion um einen AST-Typ in einen String umzuwandeln
func getTypeString(expr ast.Expr) string {
        switch t := expr.(type) {
        case *ast.Ident:
                return t.Name
        case *ast.SelectorExpr:
                if ident, ok := t.X.(*ast.Ident); ok {
                        return ident.Name + "." + t.Sel.Name
                }
        case *ast.StarExpr:
                return "*" + getTypeString(t.X)
        case *ast.ArrayType:
                return "[]" + getTypeString(t.Elt)
        case *ast.MapType:
                return "map[" + getTypeString(t.Key) + "]" + getTypeString(t.Value)
        case *ast.InterfaceType:
                return "interface{}"
        }
        return "unknown"
}

// Hilfsfunktion zum Erstellen eines Ausgabeverzeichnisses
func createOutputDir(outputDir string) error {
        if _, err := os.Stat(outputDir); os.IsNotExist(err) {
                return os.MkdirAll(outputDir, 0755)
        }
        return nil
}

// Hilfsfunktion zum Konvertieren von PUML nach PNG
func convertPumlToPng(pumlFile, pngFile string) error {
        // Methode 1: PlantUML JAR direkt nutzen (wenn Java installiert ist)
        if _, err := exec.LookPath("java"); err == nil {
                // Prüfen, ob plantuml.jar existiert
                jarPath := "plantuml.jar"
                if _, err := os.Stat(jarPath); err == nil {
                        cmd := exec.Command("java", "-jar", jarPath, pumlFile)
                        return cmd.Run()
                }
        }
        
        // Methode 2: PlantUML Server API nutzen
        pumlContent, err := os.ReadFile(pumlFile)
        if err != nil {
                return err
        }
        
        // PlantUML-Server API nutzen
        encoded := encodeForPlantUML(string(pumlContent))
        url := "http://www.plantuml.com/plantuml/png/" + encoded
        
        resp, err := http.Get(url)
        if err != nil {
                return err
        }
        defer resp.Body.Close()
        
        if resp.StatusCode != http.StatusOK {
                return fmt.Errorf("PlantUML Server antwortete mit Status %d", resp.StatusCode)
        }
        
        pngOutput, err := os.Create(pngFile)
        if err != nil {
                return err
        }
        defer pngOutput.Close()
        
        _, err = io.Copy(pngOutput, resp.Body)
        return err
}

// Hilfsfunktion zum Codieren des PlantUML-Textes für den API-Aufruf
func encodeForPlantUML(text string) string {
        // Hinweis: Dies ist eine stark vereinfachte Version des Encodings, die für kleine Diagramme funktionieren sollte
        // Für eine vollständige Implementierung wäre ein umfangreicherer Code erforderlich
        encoded := ""
        for _, c := range text {
                encoded += fmt.Sprintf("%02x", c)
        }
        return encoded
}

// Hauptfunktion zum Generieren des UML-Diagramms
func generateUMLDiagram(filePath, outputDir string) error {
        // UML-Generator erstellen und Datei parsen
        generator := NewUMLGenerator()
        if err := generator.ParseGoFile(filePath); err != nil {
                return err
        }
        
        // PlantUML-Datei generieren
        plantUML := generator.GeneratePlantUML()
        fileName := filepath.Base(filePath)
        baseName := strings.TrimSuffix(fileName, filepath.Ext(fileName))
        
        // PUML-Datei speichern
        pumlFile := filepath.Join(outputDir, baseName+".puml")
        if err := os.WriteFile(pumlFile, []byte(plantUML), 0644); err != nil {
                return fmt.Errorf("Fehler beim Schreiben der PUML-Datei: %v", err)
        }
        
        // PNG-Datei generieren
        pngFile := filepath.Join(outputDir, baseName+".png")
        if err := convertPumlToPng(pumlFile, pngFile); err != nil {
                fmt.Printf("Warnung: Konnte PNG nicht generieren - %v\n", err)
                fmt.Println("PUML-Datei wurde erstellt, aber keine PNG-Datei.")
        } else {
                fmt.Printf("UML-Diagramm als PNG generiert: %s\n", pngFile)
        }
        
        return nil
}

// Dateiwatcher
func NewFileWatcher(filePath, outputDir string) (*FileWatcher, error) {
        fileInfo, err := os.Stat(filePath)
        if err != nil {
                return nil, err
        }
        
        if err := createOutputDir(outputDir); err != nil {
                return nil, err
        }
        
        return &FileWatcher{
                filePath:     filePath,
                lastModified: fileInfo.ModTime(),
                outputDir:    outputDir,
        }, nil
}

// Überwacht Änderungen an der Datei
func (w *FileWatcher) Watch() {
        fmt.Printf("Überwache Änderungen an %s...\n", w.filePath)
        fmt.Printf("Die UML-Diagramme werden im Verzeichnis %s gespeichert\n", w.outputDir)
        
        // Initial ein Diagramm erstellen
        if err := generateUMLDiagram(w.filePath, w.outputDir); err != nil {
                fmt.Printf("Fehler bei der Generierung: %v\n", err)
        }
        
        tickChan := time.NewTicker(1 * time.Second).C
        
        for {
                select {
                case <-tickChan:
                        fileInfo, err := os.Stat(w.filePath)
                        if err != nil {
                                fmt.Printf("Fehler beim Prüfen der Datei: %v\n", err)
                                continue
                        }
                        
                        if fileInfo.ModTime().After(w.lastModified) {
                                fmt.Printf("Änderung erkannt an %s, generiere UML-Diagramm...\n", w.filePath)
                                w.lastModified = fileInfo.ModTime()
                                
                                // Kurz warten, um sicherzustellen, dass die Datei vollständig geschrieben wurde
                                time.Sleep(100 * time.Millisecond)
                                
                                if err := generateUMLDiagram(w.filePath, w.outputDir); err != nil {
                                        fmt.Printf("Fehler bei der Generierung: %v\n", err)
                                }
                        }
                }
        }
}

func ensurePlantUMLJar() {
        jarPath := "plantuml.jar"
        if _, err := os.Stat(jarPath); os.IsNotExist(err) {
                fmt.Println("Downloading PlantUML JAR...")
                url := "https://github.com/plantuml/plantuml/releases/download/v1.2023.10/plantuml-1.2023.10.jar"
                resp, err := http.Get(url)
                if err != nil {
                        fmt.Printf("Fehler beim Herunterladen: %v\n", err)
                        return
                }
                defer resp.Body.Close()
                
                if resp.StatusCode != http.StatusOK {
                        fmt.Printf("Fehler beim Herunterladen, Status: %d\n", resp.StatusCode)
                        return
                }
                
                jar, err := os.Create(jarPath)
                if err != nil {
                        fmt.Printf("Fehler beim Erstellen der JAR-Datei: %v\n", err)
                        return
                }
                defer jar.Close()
                
                _, err = io.Copy(jar, resp.Body)
                if err != nil {
                        fmt.Printf("Fehler beim Schreiben der JAR-Datei: %v\n", err)
                        return
                }
                
                fmt.Println("PlantUML JAR heruntergeladen!")
        }
}

func main() {
        fmt.Println("UML-Diagramm-Generator mit automatischer Aktualisierung")
        
        if len(os.Args) < 2 {
                fmt.Println("Verwendung: go-uml-generator <go-datei>")
                os.Exit(1)
        }
        
        // Prüfen, ob eine PlantUML JAR-Datei vorhanden ist
        ensurePlantUMLJar()
        
        filePath := os.Args[1]
        if _, err := os.Stat(filePath); os.IsNotExist(err) {
                fmt.Printf("Die Datei %s existiert nicht.\n", filePath)
                os.Exit(1)
        }
        
        // Ausgabeverzeichnis festlegen
        currentDir, err := os.Getwd()
        if err != nil {
                fmt.Printf("Fehler beim Ermitteln des aktuellen Verzeichnisses: %v\n", err)
                os.Exit(1)
        }
        
        outputDir := filepath.Join(currentDir, "uml-output")
        
        // Datei überwachen
        watcher, err := NewFileWatcher(filePath, outputDir)
        if err != nil {
                fmt.Printf("Fehler beim Erstellen des FileWatcher: %v\n", err)
                os.Exit(1)
        }
        
        // Ausgabeverzeichnis erstellen
        if err := createOutputDir(outputDir); err != nil {
                fmt.Printf("Fehler beim Erstellen des Ausgabeverzeichnisses: %v\n", err)
                os.Exit(1)
        }
        
        fmt.Printf("Ausgabeverzeichnis: %s\n", outputDir)
        fmt.Println("Drücken Sie Strg+C, um das Programm zu beenden.")
        
        // Start der Überwachung (blockiert den Thread)
        watcher.Watch()
}