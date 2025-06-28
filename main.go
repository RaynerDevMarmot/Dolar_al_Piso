package main

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os" // Importado para acceder a variables de entorno, como 'PORT'
	"strconv"
	"strings"
	"sync" // Importado para el mutex de la caché
	"time"

	"github.com/PuerkitoBio/goquery" // Librería para el web scraping
)

// PageData es la estructura que contendrá los datos que se pasan a la plantilla HTML.
type PageData struct {
	Rate               float64 // La tasa de cambio obtenida
	FormattedRate      string  // La tasa formateada para mostrar en la interfaz
	Converted          float64 // El resultado de la conversión
	FormattedConverted string  // El resultado de la conversión formateado
	Input              float64 // El monto ingresado por el usuario
	FormattedInput     string  // El monto ingresado por el usuario formateado
	Direction          string  // La dirección de la conversión (USD a Bs o Bs a USD)
	ErrorMessage       string  // Mensaje de error para mostrar al usuario
	LastUpdated        string  // Fecha y hora de la última actualización de la tasa
}

// tmpl es una variable global que almacenará la plantilla HTML ya parseada.
// Esto evita que el archivo HTML se lea y se procese en cada solicitud, mejorando el rendimiento.
var tmpl *template.Template

// --- Variables para el Mecanismo de Caché ---
var (
	cachedRate    float64    // Almacena la última tasa del dólar obtenida.
	lastFetchTime time.Time  // Registra el momento en que se obtuvo la tasa por última vez.
	cacheMutex    sync.Mutex // Protege el acceso a 'cachedRate' y 'lastFetchTime' en entornos concurrentes.
	// La tasa se considerará válida por 10 minutos. Puedes ajustar este valor si lo necesitas.
	cacheDuration = 10 * time.Minute
)

// --- Función Principal ---
func main() {
	// Define un mapa de funciones personalizadas que pueden ser usadas dentro de las plantillas HTML.
	funcMap := template.FuncMap{
		"formatMonto": FormatearMonto, // Registramos nuestra función 'FormatearMonto' con el nombre 'formatMonto'
	}

	// Parsea la plantilla HTML al iniciar la aplicación.
	// template.New("index.html") crea una nueva plantilla con un nombre.
	// .Funcs(funcMap) le añade nuestras funciones personalizadas.
	// .ParseFiles("templates/index.html") lee el contenido del archivo HTML.
	// template.Must se usa aquí para asegurar que si hay un error al parsear, la aplicación falle de inmediato.
	var err error
	tmpl, err = template.New("index.html").Funcs(funcMap).ParseFiles("templates/index.html")
	if err != nil {
		log.Fatalf("Error al cargar la plantilla: %v", err)
	}

	// Define las rutas y sus manejadores HTTP.
	// Cuando alguien acceda a la raíz del sitio "/", se ejecutará HomeHandler.
	http.HandleFunc("/", HomeHandler)
	// Sirve archivos estáticos (CSS, JS, imágenes, manifiesto, service worker) desde la carpeta "static".
	// Por ejemplo, un archivo "static/style.css" será accesible en "/static/style.css".
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	// --- Configuración del Puerto para Despliegue (Ej. Railway) ---
	// Obtiene el puerto de la variable de entorno 'PORT'.
	// Esto es crucial para plataformas como Railway que asignan un puerto dinámicamente.
	port := os.Getenv("PORT")
	if port == "" {
		// Si la variable 'PORT' no está definida (ej., en desarrollo local), usa 8080 por defecto.
		port = "8080"
	}

	// Inicia el servidor HTTP en la interfaz 0.0.0.0 (escucha en todas las IPs disponibles)
	// y en el puerto obtenido (ya sea el de Railway o 8080).
	// log.Fatal hará que la aplicación se detenga si hay un error al iniciar el servidor.
	log.Println("Servidor iniciado en http://localhost:" + port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

// --- Funciones Auxiliares ---

// FormatearMonto convierte un float64 a una cadena formateada
// con coma para los miles y punto para los decimales (formato venezolano).
func FormatearMonto(valor float64) string {
	// Formatea el número a una cadena con dos decimales, usando el punto como separador decimal inicialmente.
	// Ejemplo: 12345.678 -> "12345.68"
	s := fmt.Sprintf("%.2f", valor)

	// Divide la cadena en la parte entera y la parte decimal.
	parts := strings.Split(s, ".")
	integerPart := parts[0]
	decimalPart := "00" // Valor por defecto si no hay parte decimal
	if len(parts) > 1 {
		decimalPart = parts[1]
	}

	// Construye la parte entera con comas como separadores de miles.
	var formattedInteger strings.Builder
	nChars := len(integerPart)
	for i := 0; i < nChars; i++ {
		// Inserta una coma si no es el primer dígito y si faltan 3, 6, 9... dígitos para el final de la parte entera.
		// Esto asegura que las comas se coloquen correctamente cada tres posiciones desde la derecha.
		if i > 0 && (nChars-i)%3 == 0 {
			formattedInteger.WriteString(",")
		}
		formattedInteger.WriteByte(integerPart[i])
	}

	// Une la parte entera formateada con el separador decimal (punto) y la parte decimal.
	return fmt.Sprintf("%s.%s", formattedInteger.String(), decimalPart)
}

// GetDollarRate obtiene la tasa del dólar desde la página oficial del BCV.
// Implementa un mecanismo de caché para reducir las peticiones al BCV.
func GetDollarRate() (float64, error) {
	// Bloquea el mutex para asegurar que solo una goroutine acceda a las variables de caché a la vez.
	cacheMutex.Lock()
	defer cacheMutex.Unlock() // Asegura que el mutex se libere al final de la función, incluso si hay un error.

	// Verifica si la tasa en caché aún es válida (no ha expirado y no es cero).
	if time.Since(lastFetchTime) < cacheDuration && cachedRate != 0 {
		// Si la caché es válida, la usamos y registramos un mensaje.
		log.Printf("Usando tasa del dólar en caché: %.2f (válida por %s más)", cachedRate, cacheDuration-time.Since(lastFetchTime))
		return cachedRate, nil
	}

	// Si la caché expiró o está vacía, procedemos a hacer web scraping.
	log.Println("Caché expirada o vacía. Realizando nueva solicitud al BCV para obtener la tasa del dólar.")

	// Realiza la petición HTTP a la página del BCV.
	res, err := http.Get("https://www.bcv.org.ve/")
	if err != nil {
		log.Printf("Error al hacer la petición HTTP a BCV: %v", err)
		return 0, fmt.Errorf("error al conectar con el BCV: %w", err)
	}
	defer res.Body.Close() // Asegura que el cuerpo de la respuesta se cierre después de usarlo.

	// Verifica el código de estado de la respuesta HTTP.
	if res.StatusCode != http.StatusOK { // http.StatusOK es 200
		log.Printf("BCV respondió con estado HTTP %d", res.StatusCode)
		return 0, fmt.Errorf("error en la respuesta del BCV, estado: %d", res.StatusCode)
	}

	// Crea un nuevo documento goquery a partir del cuerpo de la respuesta HTML.
	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		log.Printf("Error al parsear el HTML de BCV: %v", err)
		return 0, fmt.Errorf("error al procesar la página del BCV: %w", err)
	}

	var rateStr string
	// Busca el elemento que contiene la tasa del dólar usando un selector CSS.
	// Este selector busca un elemento con ID 'dolar', dentro de este un elemento con clase 'centrado',
	// y finalmente, un elemento 'strong'. (Nota: Si el BCV cambia su HTML, este selector deberá actualizarse).
	doc.Find("#dolar .centrado strong").Each(func(i int, s *goquery.Selection) {
		rateStr = strings.TrimSpace(s.Text()) // Extrae el texto y elimina espacios en blanco.
	})

	// Verifica si se encontró la cadena de la tasa.
	if rateStr == "" {
		log.Print("No se encontró la cadena de la tasa del dólar con el selector especificado en la página del BCV.")
		return 0, fmt.Errorf("no se pudo encontrar la tasa del dólar en la página del BCV")
	}

	// La página del BCV usa coma (",") como separador decimal.
	// strconv.ParseFloat espera un punto (".") como separador decimal, así que lo reemplazamos.
	rateStr = strings.ReplaceAll(rateStr, ",", ".")

	// Convierte la cadena de la tasa a un número de punto flotante (float64).
	rate, err := strconv.ParseFloat(rateStr, 64)
	if err != nil {
		log.Printf("Error al convertir la tasa '%s' a float: %v", rateStr, err)
		return 0, fmt.Errorf("error al convertir la tasa '%s' a número: %w", rateStr, err)
	}

	// Si todo fue exitoso, actualizamos la caché con la nueva tasa y el tiempo actual.
	cachedRate = rate
	lastFetchTime = time.Now()
	log.Printf("Nueva tasa obtenida del BCV y guardada en caché: %.2f", cachedRate)

	return rate, nil
}

// HomeHandler es el manejador principal para las solicitudes HTTP de la aplicación.
func HomeHandler(w http.ResponseWriter, r *http.Request) {
	// Obtiene la tasa actual del dólar (usará la caché si está disponible y válida).
	rate, err := GetDollarRate()
	if err != nil {
		log.Printf("Error en HomeHandler al obtener la tasa: %v", err)
		// Si no se puede obtener la tasa, muestra un error al usuario y termina.
		http.Error(w, "No se pudo obtener la tasa del dólar en este momento. Por favor, intenta de nuevo más tarde o verifica tu conexión a internet.", http.StatusInternalServerError)
		return
	}

	// Prepara los datos iniciales para la plantilla.
	data := PageData{
		Rate:          rate,
		FormattedRate: FormatearMonto(rate), // Formatea la tasa para mostrarla en el pie de página.
		// Formato de fecha y hora para Venezuela (DD/MM/YYYY HH:MM:SS)
		LastUpdated: time.Now().Format("02/01/2006 15:04:05"),
	}

	// Maneja las solicitudes POST (cuando el usuario envía el formulario).
	if r.Method == http.MethodPost {
		amountStr := r.FormValue("amount")    // Obtiene el monto ingresado como string.
		direction := r.FormValue("direction") // Obtiene la dirección de conversión.

		// Limpia el string del monto: remueve cualquier coma para que pueda ser convertido a float.
		cleanAmountStr := strings.ReplaceAll(amountStr, ",", "")
		// Intenta convertir el monto a un número de punto flotante.
		amount, parseErr := strconv.ParseFloat(cleanAmountStr, 64)

		if parseErr == nil { // Si la conversión del monto fue exitosa
			data.Input = amount                          // Guarda el valor numérico original.
			data.FormattedInput = FormatearMonto(amount) // Formatea el monto para mostrarlo con comas en el input.
			data.Direction = direction                   // Guarda la dirección seleccionada.

			// Realiza la conversión según la dirección seleccionada.
			if direction == "usd_to_bs" {
				data.Converted = amount * rate
			} else { // Si la dirección es "bs_to_usd"
				if rate == 0 {
					// Evita la división por cero si la tasa es 0 (lo cual es raro, pero posible si el scraper falla o el BCV reporta 0).
					data.ErrorMessage = "No se puede convertir de Bs a $ con una tasa de 0. La tasa actual obtenida es 0."
					data.FormattedInput = FormatearMonto(amount) // Mantiene el monto formateado en el input.
					tmpl.Execute(w, data)                        // Renderiza la plantilla con el error.
					return
				}
				data.Converted = amount / rate
			}
			data.FormattedConverted = FormatearMonto(data.Converted) // Formatea el resultado de la conversión.

		} else { // Si la conversión del monto falló (no es un número válido)
			data.ErrorMessage = "Monto inválido. Por favor, ingresa un número válido (ej. 10.000,50)."
			data.Direction = direction      // Mantiene la dirección seleccionada por el usuario.
			data.FormattedInput = amountStr // Muestra el texto original ingresado (aunque sea inválido) para que el usuario pueda corregirlo.
		}
	} else { // Para solicitudes GET (cuando la página se carga por primera vez)
		// Inicializa el campo de input con un valor por defecto formateado.
		data.FormattedInput = "0.00" // Puedes usar "0,00" si prefieres ese estilo por defecto.
	}

	// Ejecuta la plantilla HTML, pasándole la estructura 'data' con toda la información.
	renderErr := tmpl.Execute(w, data)
	if renderErr != nil {
		log.Printf("Error al renderizar la plantilla: %v", renderErr)
		http.Error(w, "Error interno al mostrar la página. Por favor, intenta de nuevo.", http.StatusInternalServerError)
	}
}
