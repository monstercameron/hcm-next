let socket;
let reconnectAttempts = 0;
const MAX_RECONNECT_ATTEMPTS = 3;

const iframeChrome = (content) => `
    <!DOCTYPE html>
    <html lang="en">
    <head>
    <meta charset='UTF-8' />
    <meta name='viewport' content='width=device-width, initial-scale=1.0' />
    <title>UKG Panel Webpage with Chat</title>
    <script src='https://cdn.tailwindcss.com'></script>
    <script src='https://cdn.jsdelivr.net/npm/marked/marked.min.js'></script>
    <script src='https://cdn.jsdelivr.net/npm/toastify-js'></script>
    <script src='https://cdn.jsdelivr.net/npm/chart.js'></script>
    <script src='https://cdnjs.cloudflare.com/ajax/libs/react/18.2.0/umd/react.development.js'></script>
    <script src='https://cdnjs.cloudflare.com/ajax/libs/react-dom/18.2.0/umd/react-dom.development.js'></script>
    <script src='https://cdnjs.cloudflare.com/ajax/libs/htm/3.1.1/htm.min.js'></script>
    <script src="https://cdnjs.cloudflare.com/ajax/libs/three.js/r128/three.min.js"></script>

    <script
      defer
      src='https://cdn.jsdelivr.net/npm/mermaid/dist/mermaid.min.js'
    >
      const config = {
        startOnLoad: true,
        theme: 'default',
        flowchart: {
          curve: 'linear',
          useMaxWidth: true,
          htmlLabels: true,
        },
      };
      mermaid.initialize(config);
    </script>
    <link
      rel='stylesheet'
      href='https://cdn.jsdelivr.net/npm/highlight.js/styles/default.min.css'
    />
    <link
      rel='stylesheet'
      type='text/css'
      href='https://cdn.jsdelivr.net/npm/toastify-js/src/toastify.min.css'
    />
    <script>
      tailwind.config = {
        theme: {
          extend: {
            colors: {
              ukg: {
                blue: '#0066cc',
                green: '#00a650',
                orange: '#ff6a39',
              },
            },
          },
        },
      };
    </script>
    <style>
      /* Existing styles */
      @keyframes fadeIn {
        from {
          opacity: 0;
        }
        to {
          opacity: 1;
        }
      }
      @keyframes slideIn {
        from {
          transform: translateY(20px);
          opacity: 0;
        }
        to {
          transform: translateY(0);
          opacity: 1;
        }
      }
      .fade-in {
        animation: fadeIn 0.5s ease-in-out;
      }
      .slide-in {
        animation: slideIn 0.5s ease-in-out;
      }
      #app {
        display: flex;
        height: 100vh;
        overflow: hidden;
      }
      #leftPanel {
        width: 50%;
        display: flex;
        flex-direction: column;
      }
      #divider {
        width: 10px;
        cursor: col-resize;
        background-color: #d1d5db;
        transition: background-color 0.3s;
      }
      #divider:hover {
        background-color: #ff6a39;
      }
      #rightPanel {
        flex-grow: 1;
        border: none;
      }

      /* New styles for improved UI */
      .chat-message {
        margin-bottom: 1rem;
        padding: 1rem;
        border-radius: 0.5rem;
        box-shadow: 0 1px 3px rgba(0, 0, 0, 0.1);
      }
      .user-message {
        border-left: 4px solid #0066cc;
        background-color: #e6f2ff;
      }
      .ai-message {
        border-left: 4px solid #00a650;
        background-color: #e6fff2;
      }
      .system-message {
        border-left: 4px solid #ff6a39;
        background-color: #fff2e6;
      }

      /* Custom scrollbar styles */
      .custom-scrollbar::-webkit-scrollbar {
        width: 8px;
      }
      .custom-scrollbar::-webkit-scrollbar-track {
        background: #f1f1f1;
      }
      .custom-scrollbar::-webkit-scrollbar-thumb {
        background: #888;
        border-radius: 4px;
      }
      .custom-scrollbar::-webkit-scrollbar-thumb:hover {
        background: #555;
      }
    </style>
  </head>
  <body class="bg-gray-100 font-sans">
  ${content}
  </body>
</html>`;

// const injectDependencies = (iframe) => {
//   const iframeDocument =
//     iframe.contentDocument || iframe.contentWindow.document;
//   const head = iframeDocument.head;

//   // Add stylesheets
//   const stylesheets = [
//     "https://cdn.jsdelivr.net/npm/highlight.js/styles/default.min.css",
//     "https://cdn.jsdelivr.net/npm/toastify-js/src/toastify.min.css",
//   ];
//   stylesheets.forEach((href) => {
//     const link = iframeDocument.createElement("link");
//     link.rel = "stylesheet";
//     link.href = href;
//     head.appendChild(link);
//   });

//   // Add scripts
//   const scripts = [
//     "https://cdn.tailwindcss.com",
//     "https://cdn.jsdelivr.net/npm/marked/marked.min.js",
//     "https://cdn.jsdelivr.net/npm/toastify-js",
//     "https://cdn.jsdelivr.net/npm/chart.js",
//     "https://cdnjs.cloudflare.com/ajax/libs/react/18.2.0/umd/react.development.js",
//     "https://cdnjs.cloudflare.com/ajax/libs/react-dom/18.2.0/umd/react-dom.development.js",
//     "https://unpkg.com/htm@3.1.0/preact/standalone.module.js",
//     "https://cdn.jsdelivr.net/npm/mermaid/dist/mermaid.min.js",
//   ];
//   scripts.forEach((src) => {
//     const script = iframeDocument.createElement("script");
//     script.src = src;
//     head.appendChild(script);
//   });

//   console.log(head);

//   // Add inline scripts
//   // const inlineScript = iframeDocument.createElement("script");
//   // inlineScript.defer = true;
//   // inlineScript.textContent = `
//   // mermaid.initialize({
//   //   startOnLoad: true,
//   //   theme: "default",
//   //   flowchart: {
//   //     curve: "linear",
//   //     useMaxWidth: true,
//   //     htmlLabels: true,
//   //   },
//   // });
//   // tailwind.config = {
//   //   theme: {
//   //     extend: {
//   //       colors: {
//   //         ukg: {
//   //           blue: "#0066cc",
//   //           green: "#00a650",
//   //           orange: "#ff6a39",
//   //         },
//   //       },
//   //     },
//   //   },
//   // };
//   // `;
//   // head.appendChild(inlineScript);
//   // head.appendChild(style);
// };

// const injectImportMapIntoIframe = (iframe) => {
//   const iframeDocument =
//     iframe.contentDocument || iframe.contentWindow.document;
//   const importMap = iframeDocument.createElement("script");
//   importMap.type = "importmap";
//   importMap.textContent = JSON.stringify({
//     imports: {
//       three: "https://cdn.jsdelivr.net/npm/three@0.150.1/build/three.module.js",
//       "three/addons/":
//         "https://cdn.jsdelivr.net/npm/three@0.150.1/examples/jsm/",
//     },
//   });
//   iframeDocument.head.appendChild(importMap);
// };

// // Function to set up iframe listener
// const setupIframeListener = () => {
//   console.log("Setting up iframe listener");

//   const rightPanel = document.getElementById("rightPanel");
//   if (rightPanel) {
//     rightPanel.addEventListener("load", () => {
//       console.log("Right panel iframe loaded");
//       injectDependencies(rightPanel);
//       injectImportMapIntoIframe(rightPanel);
//       // You can add any other code here that needs to run after the iframe loads
//     });
//   } else {
//     console.warn("Right panel iframe not found");
//   }
// };

// Function to connect to WebSocket
const connectWebSocket = () => {
  console.log("Attempting to connect to WebSocket");
  socket = new WebSocket("ws://localhost:8080/ws");

  socket.onopen = () => {
    console.log("WebSocket connection established");
    showToast("Connected to the server!", "success");
    addChatMessage("System", "Connected to the server!");
    reconnectAttempts = 0;
  };

  socket.onmessage = (event) => {
    console.log("Received message from server:", event.data);
    addChatMessage("AI", event.data);
  };

  socket.onclose = (event) => {
    const message = event.wasClean
      ? `WebSocket connection closed cleanly, code=${event.code}, reason=${event.reason}`
      : "WebSocket connection died";
    console.log(message);

    if (reconnectAttempts < MAX_RECONNECT_ATTEMPTS) {
      showToast(
        "Disconnected from server. Attempting to reconnect...",
        "warning"
      );
      addChatMessage(
        "System",
        "Disconnected from server. Attempting to reconnect..."
      );
      setTimeout(connectWebSocket, 5000); // Try to reconnect after 5 seconds
      reconnectAttempts++;
    } else {
      showToast(
        "Failed to reconnect after multiple attempts. Please refresh the page.",
        "error"
      );
      addChatMessage(
        "System",
        "Failed to reconnect after multiple attempts. Please refresh the page."
      );
    }
  };

  socket.onerror = (error) => {
    console.log(`WebSocket error: ${error.message}`);
    showToast("Error connecting to server. Please try again later.", "error");
  };
};

// Function to send a message
const sendMessage = () => {
  // add the message to the chat history
  const chatInput = document.getElementById("chatInput");
  const message = chatInput.value.trim();
  addChatMessage("You", message);

  // all messages from the chat history get sent to the websocket
  const chatMessages = document.getElementById("chatMessages");
  const messages = Array.from(chatMessages.children).map((message) => {
    return {
      role: message.firstChild.innerText,
      content: message.lastChild.innerText,
    };
  });

  if (message && socket.readyState === WebSocket.OPEN) {
    console.log("Sending message:", message);
    socket.send(messages);
    chatInput.value = "";
  } else if (socket.readyState !== WebSocket.OPEN) {
    showToast("Cannot send message. Connection is not open.", "error");
  } else {
    showToast("Please enter a message before sending.", "warning");
  }
};

// Function to add a chat message
const addChatMessage = (sender, message) => {
  console.log("Adding chat message:", message);

  const chatMessages = document.getElementById("chatMessages");
  const messageElement = document.createElement("div");
  messageElement.className = `chat-message slide-in ${
    sender === "You"
      ? "user-message"
      : sender === "AI"
      ? "ai-message"
      : "system-message"
  }`;

  let cleanMessage = message;
  const displayContent = extractContentFromMarkdown(message, "display");

  if (sender.toLowerCase() === "AI") {
    // Extract display content
    cleanMessage = cleanMessage.replace(
      displayContent,
      "[debug: Display  Content Removed]"
    );
  }

  // Use marked.js to parse the cleaned message
  messageElement.innerHTML = `<strong>${sender}:</strong> ${marked.parse(
    cleanMessage
  )}`;

  chatMessages.appendChild(messageElement);
  chatMessages.scrollTop = chatMessages.scrollHeight;

  // Display extracted content in the right panel
  displayContentInRightPanel(displayContent);
};

// Function to extract content from markdown
const extractContentFromMarkdown = (markdown, type) => {
  const patterns = {
    display: /```display([\s\S]*?)```/g,
  };
  if (!patterns[type]) {
    throw new Error('Invalid content type. Choose "display" or "html".');
  }
  const matches = [];
  let match;
  while ((match = patterns[type].exec(markdown)) !== null) {
    matches.push(match[1].trim());
  }
  return matches.join("\n");
};

// Function to display content in the right panel
const displayContentInRightPanel = (content) => {
  const iframe = document.getElementById("rightPanel");
  iframe.src = "about:blank"; // Set src to trigger the load event
  iframe.onload = function () {
    console.log("Right panel iframe loaded");
    const doc = iframe.contentDocument;
    doc.open();
    doc.write(iframeChrome(content));
    doc.close();
  };
};

// Function to initialize resizable divider
const initResizableDivider = () => {
  const leftPanel = document.getElementById("leftPanel");
  const divider = document.getElementById("divider");
  const app = document.getElementById("app");

  let isResizing = false;
  let lastX = 0;

  const resize = (e) => {
    if (isResizing) {
      const newX = e.clientX || (e.touches && e.touches[0].clientX) || lastX;
      const newWidth = newX - app.offsetLeft;
      const minWidth = 200; // Minimum width for the left panel
      const maxWidth = app.offsetWidth - 200; // Maximum width (leaving 200px for the right panel)

      if (newWidth >= minWidth && newWidth <= maxWidth) {
        leftPanel.style.width = `${newWidth}px`;
        lastX = newX;
      }
    }
  };

  divider.addEventListener("mousedown", (e) => {
    isResizing = true;
    lastX = e.clientX;
    app.style.userSelect = "none";
  });

  divider.addEventListener("touchstart", (e) => {
    isResizing = true;
    lastX = e.touches[0].clientX;
    app.style.userSelect = "none";
    e.preventDefault(); // Prevent scrolling on touch devices
  });

  document.addEventListener("mouseup", () => {
    isResizing = false;
    app.style.userSelect = "";
  });

  document.addEventListener("touchend", () => {
    isResizing = false;
    app.style.userSelect = "";
  });

  document.addEventListener("mousemove", resize);
  document.addEventListener("touchmove", resize);

  // Throttle the resize function for better performance
  const throttledResize = throttle(resize, 10);
  document.addEventListener("mousemove", throttledResize);
  document.addEventListener("touchmove", throttledResize);
};

// Throttle function for performance optimization
function throttle(func, limit) {
  let inThrottle;
  return function () {
    const args = arguments;
    const context = this;
    if (!inThrottle) {
      func.apply(context, args);
      inThrottle = true;
      setTimeout(() => (inThrottle = false), limit);
    }
  };
}

// Function to show toast notifications
const showToast = (message, type = "info") => {
  Toastify({
    text: message,
    duration: 3000,
    gravity: "top",
    position: "right",
    background:
      type === "error"
        ? "#ff6b6b"
        : type === "warning"
        ? "#feca57"
        : type === "success"
        ? "#48dbfb"
        : "#54a0ff",
    stopOnFocus: true,
  }).showToast();
};

// setupIframeListener();

// Initialize the application
document.addEventListener("DOMContentLoaded", () => {
  connectWebSocket();
  initResizableDivider();

  // Event listeners
  document.getElementById("sendButton").addEventListener("click", sendMessage);
  document.getElementById("chatInput").addEventListener("keypress", (e) => {
    if (e.key === "Enter") {
      sendMessage();
    }
  });

  // Confirmation before closing the page
  window.addEventListener("beforeunload", (e) => {
    e.preventDefault();
    e.returnValue = "";
  });
});
