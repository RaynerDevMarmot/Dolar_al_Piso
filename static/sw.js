// static/sw.js

const CACHE_NAME = 'bcv-converter-cache-v1'; // Nombre de la caché (incrementa para actualizar)
const urlsToCache = [
  '/', // La página principal
  '/static/manifest.json', // El manifiesto de la PWA
  '/static/sw.js', // El propio service worker
  '/static/icons/icon-192x192.png', // Un ícono (asegúrate de que existan)
  '/static/icons/icon-512x512.png', // Otro ícono
  'https://cdn.jsdelivr.net/npm/bootstrap@5.3.0/dist/css/bootstrap.min.css', // CSS de Bootstrap
  'https://cdnjs.cloudflare.com/ajax/libs/font-awesome/6.5.0/css/all.min.css' // CSS de Font Awesome
];

// Evento 'install': Se dispara cuando el Service Worker se instala por primera vez.
self.addEventListener('install', event => {
  event.waitUntil(
    caches.open(CACHE_NAME)
      .then(cache => {
        console.log('Service Worker: Abriendo caché y precargando recursos.');
        return cache.addAll(urlsToCache);
      })
      .then(() => self.skipWaiting()) // Fuerza la activación inmediata del SW
      .catch(error => {
        console.error('Service Worker: Falló la precarga de recursos:', error);
      })
  );
});

// Evento 'activate': Se dispara cuando el Service Worker se activa.
self.addEventListener('activate', event => {
  event.waitUntil(
    caches.keys().then(cacheNames => {
      return Promise.all(
        cacheNames.map(cacheName => {
          // Elimina cachés antiguas que no coincidan con el nombre actual
          if (cacheName !== CACHE_NAME) {
            console.log('Service Worker: Eliminando caché antigua:', cacheName);
            return caches.delete(cacheName);
          }
        })
      );
    }).then(() => self.clients.claim()) // Permite al SW controlar las páginas inmediatamente
  );
});

// Evento 'fetch': Se dispara en cada solicitud de red de la página controlada.
self.addEventListener('fetch', event => {
  // Solo interceptamos peticiones GET (para recursos)
  if (event.request.method === 'GET') {
    event.respondWith(
      caches.match(event.request) // Intenta encontrar el recurso en la caché
        .then(response => {
          // Si el recurso está en caché, lo devuelve
          if (response) {
            console.log('Service Worker: Sirviendo desde caché:', event.request.url);
            return response;
          }
          // Si no está en caché, va a la red
          console.log('Service Worker: Sirviendo desde red:', event.request.url);
          return fetch(event.request)
            .then(networkResponse => {
              // Si la respuesta de la red es válida, la añade a la caché para futuros usos
              if (networkResponse && networkResponse.status === 200 && networkResponse.type === 'basic') {
                const responseToCache = networkResponse.clone();
                caches.open(CACHE_NAME)
                  .then(cache => {
                    cache.put(event.request, responseToCache);
                  });
              }
              return networkResponse;
            })
            .catch(error => {
              console.error('Service Worker: Falló la petición de red:', event.request.url, error);
              // Podrías devolver una página offline aquí si la petición falla y no hay caché
              // return caches.match('/offline.html'); // Requiere crear offline.html
            });
        })
    );
  } else {
    // Para otras peticiones (POST), simplemente las deja pasar a la red.
    return fetch(event.request);
  }
});