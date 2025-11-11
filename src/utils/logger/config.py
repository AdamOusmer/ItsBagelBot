import time

#%% Default Directories
OUT_DIRECTORY = '../out/out'
LOGGER_DIRECTORY = '../out/logs'

#%% Output

# The output file name should be dynamic, otherwise the output will be overwritten.
OUT_NAME = f"flattened-{time.strftime("%Y-%m-%d--%H-%M-%S")}"

#%% Logging

# To access all steps one by one, including each addition/removal, configure the log level to DEBUG. This mode can be
# achieved with the -t flag
# This variable is used to set the default log level when -t is not specified.
# The possible values are: DEBUG, INFO, WARNING, ERROR, CRITICAL
LOGGER_LEVEL = 'DEBUG'
LOGGER_NAME = 'main'

# IMPORTANT: The %(message)s is required to display the message.
LOGGER_FORMAT = '<%(levelname)s> - [%(name)s]: %(message)s\n'